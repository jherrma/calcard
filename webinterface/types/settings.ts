export interface UserProfileStats {
  calendar_count: number;
  contact_count: number;
  app_password_count: number;
}

export interface UserProfile {
  id: string;
  email: string;
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
