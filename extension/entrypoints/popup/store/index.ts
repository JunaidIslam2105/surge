/**
 * Reactive store using SolidJS signals.
 * Uses plain objects/arrays instead of Map for proper Solid reactivity.
 */

import { createSignal } from 'solid-js';
import { MB } from '../lib/utils';
import type { ServerProfile } from '../../../lib/storage';
import type {
  DownloadStatus,
  HistoryEntry,
  ProgressMsg,
  DownloadStartedMsg,
  DownloadCompleteMsg,
  DownloadErrorMsg,
  DownloadPausedMsg,
  DownloadResumedMsg,
  DownloadQueuedMsg,
  DownloadRemovedMsg,
} from './types';

// Active downloads: updated via SSE events and periodic polling.
const [activeDownloads, setActiveDownloads] = createSignal<DownloadStatus[]>([]);
export { activeDownloads, setActiveDownloads };

function sameDownloadStatus(left: DownloadStatus, right: DownloadStatus): boolean {
  return left.id === right.id
    && left.url === right.url
    && left.filename === right.filename
    && left.dest_path === right.dest_path
    && left.total_size === right.total_size
    && left.downloaded === right.downloaded
    && left.progress === right.progress
    && left.speed === right.speed
    && left.status === right.status
    && left.error === right.error
    && left.eta === right.eta
    && left.connections === right.connections
    && left.added_at === right.added_at
    && left.time_taken === right.time_taken
    && left.avg_speed === right.avg_speed;
}

export function reconcileActiveDownloads(nextDownloads: DownloadStatus[]): void {
  setActiveDownloads((prevDownloads) => {
    const prevById = new Map(prevDownloads.map((download) => [download.id, download]));
    const merged = nextDownloads.map((download) => {
      const prev = prevById.get(download.id);
      if (!prev) return download;
      const next = { ...prev, ...download };
      return sameDownloadStatus(prev, next) ? prev : next;
    });

    const hasChanged = merged.length !== prevDownloads.length
      || merged.some((download, index) => prevDownloads[index] !== download);

    return hasChanged ? merged : prevDownloads;
  });
}

export function upsertActiveDownload(dl: DownloadStatus): void {
  setActiveDownloads(prev => {
    const idx = prev.findIndex(d => d.id === dl.id);
    if (idx >= 0) {
      const next = [...prev];
      next[idx] = { ...next[idx], ...dl };
      return next;
    }
    return [...prev, dl];
  });
}

export function removeActiveDownload(id: string): void {
  setActiveDownloads(prev => prev.filter(d => d.id !== id));
}

// History downloads: capped to 100 entries by background handler.
const [historyDownloads, setHistoryDownloads] = createSignal<HistoryEntry[]>([]);
export { historyDownloads, setHistoryDownloads };

// Server connection state
const [serverConnected, setServerConnected] = createSignal(false);
export { serverConnected, setServerConnected };

// Browser download interception state
const [interceptEnabled, setInterceptEnabled] = createSignal(true);
export { interceptEnabled, setInterceptEnabled };
const [notificationsEnabled, setNotificationsEnabled] = createSignal(true);
export { notificationsEnabled, setNotificationsEnabled };
const [fallbackToBrowser, setFallbackToBrowser] = createSignal(true);
export { fallbackToBrowser, setFallbackToBrowser };
const [minFileSize, setMinFileSize] = createSignal(10);
export { minFileSize, setMinFileSize };

// Surge server URL for API requests (resolved from the active server profile)
const [serverUrl, setServerUrl] = createSignal('');
export { serverUrl, setServerUrl };
const [serverUrlLocked, setServerUrlLocked] = createSignal(false);
export { serverUrlLocked, setServerUrlLocked };

// Named server profiles and the active selection
const [serverProfiles, setServerProfiles] = createSignal<ServerProfile[]>([]);
export { serverProfiles, setServerProfiles };
const [activeProfileId, setActiveProfileId] = createSignal('');
export { activeProfileId, setActiveProfileId };

const [authToken, setAuthToken] = createSignal('');
export { authToken, setAuthToken };
const [authTokenLocked, setAuthTokenLocked] = createSignal(false);
export { authTokenLocked, setAuthTokenLocked };

// Derived from token validity, not directly from token presence
const [authValid, setAuthValid] = createSignal(false);
export { authValid, setAuthValid };

// Current UI view selection
export type ViewMode = 'active' | 'history' | 'settings';
const [currentView, setCurrentView] = createSignal<ViewMode>('active');
export { currentView, setCurrentView };

// Maps SSE event types to store mutations

function updateActiveDownload(id: string, update: (download: DownloadStatus) => DownloadStatus): void {
  setActiveDownloads((downloads) => {
    const index = downloads.findIndex((download) => download.id === id);
    if (index < 0) return downloads;

    const next = [...downloads];
    next[index] = update(downloads[index]);
    return next;
  });
}

function createDownload(
  message: DownloadStartedMsg | DownloadQueuedMsg,
  status: DownloadStatus['status'],
  totalSize = 0,
): DownloadStatus {
  return {
    id: message.DownloadID,
    url: message.URL,
    filename: message.Filename,
    dest_path: message.DestPath,
    total_size: totalSize,
    downloaded: 0,
    progress: 0,
    speed: 0,
    status,
    eta: 0,
    connections: 0,
    added_at: Date.now(),
    time_taken: 0,
    avg_speed: 0,
  };
}

function calculateProgress(downloaded: number, totalSize: number): number {
  return totalSize > 0 ? (downloaded / totalSize) * 100 : 0;
}

export function handleSseEvent(event: string, data: unknown): void {
  switch (event) {
    case 'progress': {
      const message = data as ProgressMsg;
      updateActiveDownload(message.DownloadID, (download) => {
        const totalSize = message.Total || download.total_size;
        return {
          ...download,
          downloaded: message.Downloaded,
          progress: calculateProgress(message.Downloaded, totalSize),
          speed: message.Speed / MB,
          eta: message.Speed > 0 ? Math.ceil((totalSize - message.Downloaded) / message.Speed) : 0,
          connections: message.ActiveConnections,
          total_size: totalSize,
        };
      });
      break;
    }
    case 'started': {
      const message = data as DownloadStartedMsg;
      const existing = activeDownloads().find((download) => download.id === message.DownloadID);
      if (existing) {
        upsertActiveDownload({
          ...existing,
          status: 'downloading',
          downloaded: 0,
          progress: 0,
          total_size: message.Total,
          dest_path: message.DestPath,
        });
      } else {
        upsertActiveDownload(createDownload(message, 'downloading', message.Total));
      }
      break;
    }
    case 'complete': {
      const message = data as DownloadCompleteMsg;
      updateActiveDownload(message.DownloadID, (download) => ({
        ...download,
        status: 'completed',
        progress: 100,
        downloaded: message.Total,
        avg_speed: message.AvgSpeed,
        time_taken: message.Elapsed,
      }));
      break;
    }
    case 'error': {
      const message = data as DownloadErrorMsg;
      updateActiveDownload(message.DownloadID, (download) => ({
        ...download,
        status: 'error',
        error: message.Err,
      }));
      break;
    }
    case 'paused': {
      const message = data as DownloadPausedMsg;
      updateActiveDownload(message.DownloadID, (download) => ({
        ...download,
        status: 'paused',
        downloaded: message.Downloaded,
        progress: calculateProgress(message.Downloaded, download.total_size),
      }));
      break;
    }
    case 'resumed': {
      const message = data as DownloadResumedMsg;
      updateActiveDownload(message.DownloadID, (download) => ({
        ...download,
        status: 'downloading',
      }));
      break;
    }
    case 'queued': {
      const message = data as DownloadQueuedMsg;
      const existing = activeDownloads().some((download) => download.id === message.DownloadID);
      if (!existing) {
        upsertActiveDownload(createDownload(message, 'queued'));
      }
      break;
    }
    case 'removed': {
      const msg = data as DownloadRemovedMsg;
      removeActiveDownload(msg.DownloadID);
      break;
    }
  }
}
