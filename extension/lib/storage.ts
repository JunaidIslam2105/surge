export const STORAGE_KEYS = {
  INTERCEPT: 'interceptEnabled',
  TOKEN: 'authToken',
  VERIFIED: 'authVerified',
  SERVER_URL: 'serverUrl',
  DISCOVERED_SERVER_URL: 'discoveredServerUrl',
  NOTIFICATIONS: 'notificationsEnabled',
  FALLBACK_TO_BROWSER: 'fallbackToBrowser',
  MIN_FILE_SIZE: 'minFileSize',
  PROFILES: 'serverProfiles',
  ACTIVE_PROFILE_ID: 'activeServerProfileId',
} as const;

export function readStoredString(
  values: Record<string, unknown>,
  key: string,
): string {
  const value = values[key];
  return typeof value === 'string' ? value : '';
}

export function readStoredBoolean(
  values: Record<string, unknown>,
  key: string,
  fallback: boolean,
): boolean {
  const value = values[key];

  if (typeof value === 'boolean') return value;
  if (value === 'true') return true;
  if (value === 'false') return false;
  return fallback;
}

export function readStoredNumber(
  values: Record<string, unknown>,
  key: string,
  fallback: number,
): number {
  const value = values[key];
  if (typeof value === 'number') return value;
  if (typeof value === 'string') {
    const parsed = parseFloat(value);
    if (!isNaN(parsed)) return parsed;
  }
  return fallback;
}

// ---------------------------------------------------------------------------
// Server profiles
//
// A profile is a named (name + URL) Surge server. The extension persists a
// list of profiles plus an active-profile id, and the rest of the extension
// resolves the active profile's URL instead of the single legacy SERVER_URL.
// The legacy SERVER_URL key is kept for backward compatibility and is migrated
// into a default profile on first read (see migrateServerProfiles).
// ---------------------------------------------------------------------------

export interface ServerProfile {
  id: string;
  name: string;
  url: string;
  token: string;
}

export interface ServerProfilesState {
  profiles: ServerProfile[];
  activeId: string;
}

// Canonical URL normalization shared by popup and background contexts.
// Exported so that popup/lib/utils.ts can re-export it without duplicating the logic.
export function normalizeServerUrl(url: string): string {
  const trimmed = (url ?? '').trim();
  if (!trimmed) return '';
  const withScheme = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
  return withScheme.replace(/\/+$/, '');
}

function isServerProfile(value: unknown): value is ServerProfile {
  if (typeof value !== 'object' || value === null) return false;
  const record = value as Record<string, unknown>;
  return typeof record.id === 'string'
    && record.id.length > 0
    && typeof record.name === 'string'
    && typeof record.url === 'string';
}

/** Read and sanitize the stored profiles array, normalizing URLs and dropping malformed entries. */
export function parseServerProfiles(values: Record<string, unknown>): ServerProfile[] {
  const raw = values[STORAGE_KEYS.PROFILES];
  if (!Array.isArray(raw)) return [];
  return raw
    .filter(isServerProfile)
    .map((profile) => ({
      id: profile.id,
      name: profile.name,
      url: normalizeServerUrl(profile.url),
      token: profile.token || '',
    }));
}

/** Resolve the active profile by id, falling back to the first profile, or null when empty. */
export function resolveActiveProfile(
  profiles: ServerProfile[],
  activeId: string,
): ServerProfile | null {
  if (profiles.length === 0) return null;
  return profiles.find((profile) => profile.id === activeId) ?? profiles[0];
}

/** Resolve the active profile's URL. Empty string when there is no active profile. */
export function resolveActiveServerUrl(profiles: ServerProfile[], activeId: string): string {
  return resolveActiveProfile(profiles, activeId)?.url ?? '';
}

/** Resolve the active profile's token. Empty string when there is no active profile. */
export function resolveActiveServerToken(profiles: ServerProfile[], activeId: string): string {
  return resolveActiveProfile(profiles, activeId)?.token ?? '';
}

let profileIdCounter = 0;

function generateProfileId(): string {
  profileIdCounter += 1;
  const random = Math.random().toString(36).slice(2, 8);
  return `profile_${Date.now().toString(36)}_${profileIdCounter}_${random}`;
}

/**
 * Append a new profile (with a generated id) and make it the active one.
 * Throws if the URL is empty after normalization.
 */
export function addServerProfile(
  profiles: ServerProfile[],
  input: { name: string; url: string; token: string },
): ServerProfilesState {
  const url = normalizeServerUrl(input.url);
  const profile: ServerProfile = {
    id: generateProfileId(),
    name: input.name.trim() || 'Server',
    url,
    token: input.token || '',
  };
  const next = [...profiles, profile];
  return { profiles: next, activeId: profile.id };
}

/** Remove a profile, re-pointing the active id to the first remaining profile when needed. */
export function removeServerProfile(
  profiles: ServerProfile[],
  removeId: string,
  activeId: string,
): ServerProfilesState {
  const next = profiles.filter((profile) => profile.id !== removeId);
  if (next.length === 0) return { profiles: next, activeId: '' };
  const stillActive = next.some((profile) => profile.id === activeId);
  return { profiles: next, activeId: stillActive ? activeId : next[0].id };
}

/**
 * Backward-compatibility migration. When no profiles are stored yet but a legacy
 * single SERVER_URL exists, wrap it in a "Default" profile and make it active.
 * Returns migrated: false when nothing needs to change.
 */
export function migrateServerProfiles(values: Record<string, unknown>): ServerProfilesState & { migrated: boolean } {
  let existing = parseServerProfiles(values);
  const legacyToken = readStoredString(values, STORAGE_KEYS.TOKEN);

  if (existing.length > 0) {
    const storedActiveId = readStoredString(values, STORAGE_KEYS.ACTIVE_PROFILE_ID);
    const activeId = resolveActiveProfile(existing, storedActiveId)?.id ?? '';

    let migrated = false;
    if (activeId && legacyToken) {
      const activeIdx = existing.findIndex(p => p.id === activeId);
      if (activeIdx !== -1 && !existing[activeIdx].token) {
        existing = [...existing];
        existing[activeIdx] = { ...existing[activeIdx], token: legacyToken };
        migrated = true;
      }
    }

    return {
      profiles: existing,
      activeId,
      migrated,
    };
  }

  const legacyUrl = normalizeServerUrl(readStoredString(values, STORAGE_KEYS.SERVER_URL));
  if (!legacyUrl) {
    return { profiles: [], activeId: '', migrated: false };
  }

  const { profiles, activeId } = addServerProfile([], { name: 'Default', url: legacyUrl, token: legacyToken });
  return { profiles, activeId, migrated: true };
}
