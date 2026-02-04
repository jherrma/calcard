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
  expires_in: number;
}

export interface RefreshResponse {
  access_token: string;
  expires_in: number;
}

export interface SystemSettings {
  admin_configured: boolean;
  smtp_enabled: boolean;
  registration_enabled: boolean;
}
