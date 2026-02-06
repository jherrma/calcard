export interface User {
  id: string;
  email: string;
  username: string;
  display_name?: string;
  is_admin: boolean;
  avatar_url?: string;
  created_at: string;
}

export interface LoginResponse {
  user: User;
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_at: number;
}

export interface RefreshResponse {
  access_token: string;
  token_type: string;
  expires_at: number;
}

export interface SystemSettings {
  admin_configured: boolean;
  smtp_enabled: boolean;
  registration_enabled: boolean;
}

export interface AuthMethod {
  id: string;
  type: 'local' | 'oauth2' | 'oidc' | 'saml';
  name: string;
  url?: string; // For external providers, the initiation URL
  icon?: string; // Optional icon identifier
}

export interface AuthMethodsResponse {
  methods: AuthMethod[];
}
