import { createSignal, For, Show } from 'solid-js';
import { STORAGE_KEYS } from '../../../lib/storage';
import {
  setServerUrl,
  setServerUrlLocked,
  serverConnected,
  serverProfiles, setServerProfiles,
  activeProfileId, setActiveProfileId,
  setAuthToken,
  setAuthTokenLocked,
  authValid, setAuthValid,
  interceptEnabled, setInterceptEnabled,
  notificationsEnabled, setNotificationsEnabled,
  fallbackToBrowser, setFallbackToBrowser,
  minFileSize, setMinFileSize,
} from '../store';
import {
  handleAddProfile as _handleAddProfile,
  handleSwitchProfile as _handleSwitchProfile,
  handleDeleteProfile as _handleDeleteProfile,
} from '../lib/settings-handlers';

function saveStatusSignal() {
  const [status, setStatus] = createSignal('');
  let timer: ReturnType<typeof setTimeout> | null = null;
  const show = (msg: string, ms = 2000) => {
    if (timer) clearTimeout(timer);
    setStatus(msg);
    if (ms > 0) timer = setTimeout(() => setStatus(''), ms);
  };
  return [status, show] as const;
}

export default function SettingsView() {
  const [serverStatus, showServerStatus] = saveStatusSignal();
  const [newProfileName, setNewProfileName] = createSignal('');
  const [newProfileUrl, setNewProfileUrl] = createSignal('');
  const [newProfileToken, setNewProfileToken] = createSignal('');
  const [isConnecting, setIsConnecting] = createSignal(false);
  const [connectionValid, setConnectionValid] = createSignal(false);

  const extensionVersion = browser.runtime.getManifest().version;
  const isFirefox = (browser.runtime.getURL as (path?: string) => string)('').startsWith('moz-extension:');

  const makeStore = () => ({
    getProfiles: serverProfiles,
    getActiveId: activeProfileId,
    setProfiles: setServerProfiles,
    setActiveId: setActiveProfileId,
    setServerUrl,
    setServerUrlLocked,
    setAuthToken,
    setAuthTokenLocked,
    setAuthValid,
  });

  const handleConnect = async () => {
    const url = newProfileUrl().trim();
    const token = newProfileToken().trim();
    if (!token) {
      showServerStatus('Enter an Auth Token');
      return;
    }

    setIsConnecting(true);
    showServerStatus('Connecting...', 0); // Keep showing until done
    
    try {
      const res = await browser.runtime.sendMessage({ type: 'testConnection', url, token }).catch(() => null) as { ok?: boolean; url?: string; error?: string; mode?: string } | null;
      if (res?.ok) {
        setConnectionValid(true);
        if (!newProfileName().trim()) {
          if (res.mode === 'service') setNewProfileName('Surge System Service');
          else if (res.mode === 'local') setNewProfileName('Surge Local');
        }
        showServerStatus('Connected');
      } else if (res?.error === 'invalid_token_service') {
        showServerStatus('Invalid System Service Token');
      } else if (res?.error === 'invalid_token') {
        showServerStatus('Invalid Token');
      } else {
        showServerStatus('Server unavailable');
      }
    } catch {
      showServerStatus('Server unavailable');
    } finally {
      setIsConnecting(false);
    }
  };

  const handleSaveProfile = async () => {
    showServerStatus('Saving...', 0);
    const result = await _handleAddProfile(
      { name: newProfileName(), url: newProfileUrl(), token: newProfileToken() },
      makeStore(),
      browser.storage.local,
      true, // Set authValid = true because we just successfully connected!
    );
    if (result.ok) {
      setNewProfileName('');
      setNewProfileUrl('');
      setNewProfileToken('');
      setConnectionValid(false);
      showServerStatus('Saved');
    } else {
      showServerStatus(result.error ?? 'Failed to save');
    }
  };

  const handleSwitchProfile = async (profileId: string) => {
    showServerStatus('Switching...', 0);
    const result = await _handleSwitchProfile(profileId, makeStore(), browser.storage.local);
    showServerStatus(result.ok ? 'Switched' : (result.error ?? 'Failed to switch'));
  };

  const handleDeleteProfile = async () => {
    showServerStatus('Removing...', 0);
    const result = await _handleDeleteProfile(makeStore(), browser.storage.local);
    showServerStatus(result.ok ? 'Removed' : (result.error ?? 'Failed to remove'));
  };

  const handleInterceptToggle = async (checked: boolean) => {
    setInterceptEnabled(checked);
    await browser.storage.local.set({ [STORAGE_KEYS.INTERCEPT]: checked });
  };
  const handleNotificationsToggle = async (checked: boolean) => {
    setNotificationsEnabled(checked);
    await browser.storage.local.set({ [STORAGE_KEYS.NOTIFICATIONS]: checked });
  };
  const handleFallbackToggle = async (checked: boolean) => {
    setFallbackToBrowser(checked);
    await browser.storage.local.set({ [STORAGE_KEYS.FALLBACK_TO_BROWSER]: checked });
  };
  const handleMinFileSizeChange = async (value: string) => {
    const num = parseFloat(value);
    if (!isNaN(num) && num >= 0) {
      setMinFileSize(num);
      await browser.storage.local.set({ [STORAGE_KEYS.MIN_FILE_SIZE]: num });
    } else if (value === '') {
      setMinFileSize(0);
      await browser.storage.local.set({ [STORAGE_KEYS.MIN_FILE_SIZE]: 0 });
    }
  };

  return (
    <div>
      <div class="settings-group">
        <label class="toggle-row">
          <span>Intercept Downloads</span>
          <div class="toggle">
            <input
              type="checkbox"
              checked={interceptEnabled()}
              onChange={(e) => { void handleInterceptToggle((e.target as HTMLInputElement).checked); }}
            />
            <span class="toggle-slider" />
          </div>
        </label>
        <label class="toggle-row">
          <span>Show Notifications</span>
          <div class="toggle">
            <input
              type="checkbox"
              checked={notificationsEnabled()}
              onChange={(e) => { void handleNotificationsToggle((e.target as HTMLInputElement).checked); }}
            />
            <span class="toggle-slider" />
          </div>
        </label>
        <label class="toggle-row">
          <span>Retry failed downloads in browser</span>
          <div class="toggle">
            <input
              type="checkbox"
              checked={fallbackToBrowser()}
              onChange={(e) => { void handleFallbackToggle((e.target as HTMLInputElement).checked); }}
            />
            <span class="toggle-slider" />
          </div>
        </label>
        <div class="settings-field" style="margin-top: 1rem;">
          <label class="settings-label" for="min-file-size">Minimum File Size to Intercept (MB)</label>
          <div class="auth-input settings-input-row" style="max-width: 120px;">
            <input
              id="min-file-size"
              type="number"
              min="0"
              step="1"
              value={minFileSize() || ''}
              onInput={(e) => { void handleMinFileSizeChange((e.target as HTMLInputElement).value); }}
            />
          </div>
          <div class="settings-help">
            Downloads smaller than this will be handled by the browser. Leave 0 to intercept all.
          </div>
        </div>
      </div>

      <div class="settings-group">
        <h3 class="settings-group-title">Server</h3>

        <Show when={serverProfiles().length > 0}>
          <div class="settings-field">
            <label class="settings-label" for="server-profile">Active Server</label>
            <div class="auth-input settings-input-row">
              <select
                id="server-profile"
                value={activeProfileId()}
                onChange={(e) => { void handleSwitchProfile((e.target as HTMLSelectElement).value); }}
              >
                <For each={serverProfiles()}>
                  {(profile) => (
                    <option value={profile.id}>{profile.name} ({profile.url || 'localhost'})</option>
                  )}
                </For>
              </select>
              <button onClick={() => { void handleDeleteProfile(); }}>Delete</button>
            </div>
            <div class="settings-help" style="margin-top: 10px; display: flex; align-items: center; gap: 6px;">
              <strong>Status:</strong> {
                !serverConnected() ? <span class="auth-status err" style="margin: 0;">Disconnected</span> :
                !authValid() ? <span class="auth-status err" style="margin: 0;">Invalid Token</span> :
                <span class="auth-status ok" style="margin: 0;">Connected</span>
              }
            </div>
          </div>
        </Show>

        <div class="settings-field" style={serverProfiles().length > 0 ? "margin-top: 24px; padding-top: 16px; border-top: 1px solid var(--border-subtle);" : ""}>
          <label class="settings-label">
            {serverProfiles().length > 0 ? 'Add a Server' : 'Connect to a Server'}
          </label>
          
          <div class="auth-input settings-input-row" style="margin-bottom: 8px;">
            <input
              id="profile-url"
              type="text"
              value={newProfileUrl()}
              placeholder="Server URL (e.g. http://127.0.0.1:1700)"
              onInput={(e) => { setNewProfileUrl((e.target as HTMLInputElement).value); setConnectionValid(false); }}
              disabled={isConnecting()}
            />
          </div>
          
          <div class="auth-input settings-input-row">
            <input
              id="profile-token"
              type="password"
              value={newProfileToken()}
              placeholder="Auth Token"
              onInput={(e) => { setNewProfileToken((e.target as HTMLInputElement).value); setConnectionValid(false); }}
              disabled={isConnecting()}
            />
            <button onClick={() => { void handleConnect(); }} disabled={isConnecting() || !newProfileToken()}>
              {isConnecting() ? '...' : 'Connect'}
            </button>
          </div>
          
          {serverStatus() && !connectionValid() && (
            <div class={`auth-status below${serverStatus() === 'Connected' || serverStatus() === 'Saved' || serverStatus() === 'Removed' || serverStatus() === 'Switched' ? ' ok' : serverStatus().endsWith('...') ? '' : ' err'}`}>{serverStatus()}</div>
          )}

          <Show when={connectionValid()}>
            <div style="margin-top: 16px; padding: 14px; background: var(--bg-surface); border-radius: var(--radius-md); border: 1px solid var(--border-default);">
              <label class="settings-label" for="profile-name" style="margin-bottom: 10px;">Save Profile</label>
              <div class="auth-input settings-input-row">
                <input
                  id="profile-name"
                  type="text"
                  value={newProfileName()}
                  placeholder="Name (e.g. Home PC)"
                  onInput={(e) => { setNewProfileName((e.target as HTMLInputElement).value); }}
                />
                <button onClick={() => { void handleSaveProfile(); }}>Save</button>
              </div>
              {serverStatus() && (
                 <div class={`auth-status below${serverStatus() === 'Saved' ? ' ok' : serverStatus().endsWith('...') ? '' : ' err'}`}>{serverStatus()}</div>
              )}
            </div>
          </Show>
        </div>
      </div>

      <div class="settings-group">
        <div class="settings-group-header">
          <h3 class="settings-group-title">Support</h3>
          <div class="version-badge">v{extensionVersion}</div>
        </div>
        <a
          href="https://github.com/SurgeDM/Surge"
          target="_blank"
          rel="noopener noreferrer"
          class="support-link"
        >
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
          </svg>
          SurgeDM/Surge
        </a>
        <a
          href="https://github.com/SurgeDM/Surge/issues/new?template=extension_bug_report.md"
          target="_blank"
          rel="noopener noreferrer"
          class="support-link"
        >
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="m8 2 1.88 1.88" />
            <path d="M14.12 3.88 16 2" />
            <path d="M9 7.13v-1a3.003 3.003 0 1 1 6 0v1" />
            <path d="M12 20c-3.3 0-6-2.7-6-6v-3a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v3c0 3.3-2.7 6-6 6" />
            <path d="M12 20v-9" />
            <path d="M6.53 9C4.6 8.8 3 7.1 3 5" />
            <path d="M6 13H2" />
            <path d="M3 21c0-2.1 1.7-3.9 3.8-4" />
            <path d="M20.97 5c0 2.1-1.6 3.8-3.5 4" />
            <path d="M22 13h-4" />
            <path d="M17.2 17c2.1.1 3.8 1.9 3.8 4" />
          </svg>
          Report a Bug
        </a>
        {isFirefox && (
          <a
            href="https://addons.mozilla.org/en-US/firefox/addon/surge"
            target="_blank"
            rel="noopener noreferrer"
            class="support-link"
          >
            Firefox Add-ons
          </a>
        )}
      </div>
    </div>
  );
}
