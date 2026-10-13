export interface UserProfileStats {
  calendar_count: number;
  contact_count: number;
  app_password_count: number;
}

export interface UserProfile {
  id: string;
  email: string;
  // The account username: the {username} segment of the DAV URLs.
  username: string;
  display_name: string;
  is_active: boolean;
  email_verified: boolean;
  created_at: string;
  updated_at: string;
  auth_methods: string[];
  stats: UserProfileStats;
}

export interface AppPassword {
  id: string;
  name: string;
  scopes: string[];
  created_at: string;
  last_used_at?: string;
  last_used_ip?: string;
}

export interface AppPasswordCredentials {
  username: string;
  password: string;
  server_url: string;
}

export interface CreateAppPasswordResponse {
  id: string;
  name: string;
  scopes: string[];
  created_at: string;
  password: string;
  credentials: AppPasswordCredentials;
}

export interface DavCredential {
  id: string;
  name: string;
  username: string;
  permission: string;
  expires_at?: string;
  created_at: string;
  last_used_at?: string;
  last_used_ip?: string;
}

export interface DavCredentialListResponse {
  credentials: DavCredential[];
}

export interface DavCredentialCreateResponse {
  id: string;
  name: string;
  username: string;
  permission: string;
  expires_at?: string;
  created_at: string;
}

export interface LinkedProvider {
  provider: string;
  email: string;
  linked_at: string;
}

export interface LinkedProvidersResponse {
  providers: LinkedProvider[];
  has_password: boolean;
}

export interface ChangePasswordResponse {
  message: string;
  access_token: string;
}

// User preferences (story 103). Values are strings on the wire because the
// backend stores them in a generic key/value table; the preferences store parses
// them into usable types.

export type TimeFormat = '12h' | '24h';

/**
 * The weekday a week starts on, numbered like `Date.getDay()` and FullCalendar's
 * `firstDay`: 0 Sunday, 1 Monday, 6 Saturday. Sent as the string `"0"`/`"1"`/`"6"`.
 */
export type FirstDayOfWeek = 0 | 1 | 6;

export interface PreferencesResponse {
  preferences: Record<string, string>;
}

// MCP access tokens (story 104). These authenticate MCP clients against the
// `/mcp` endpoint; unlike the JWT the web UI holds, they are long-lived and
// revoked individually.
//
// There is no `token` field here on purpose: the secret exists only in
// MCPTokenCreateResponse, which the server can produce exactly once.
export interface MCPToken {
  id: string;
  name: string;
  token_prefix: string;
  expires_at: string | null;
  last_used_at: string | null;
  last_used_ip: string;
  created_at: string;
}

export interface MCPTokenListResponse {
  tokens: MCPToken[];
}

export interface MCPTokenCreateResponse {
  id: string;
  name: string;
  /** The full secret. Shown once, never retrievable again. */
  token: string;
  token_prefix: string;
  expires_at: string | null;
  created_at: string;
}
