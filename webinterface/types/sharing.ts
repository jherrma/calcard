export interface ShareUser {
  id: string;
  username: string;
  display_name: string;
  email: string;
}

export interface CalendarShare {
  id: string;
  calendar_id: string;
  shared_with: ShareUser;
  permission: string;
  created_at: string;
}

export interface AddressBookShare {
  id: string;
  addressbook_id: string;
  shared_with: ShareUser;
  permission: string;
  created_at: string;
}

export interface PublicAccessStatus {
  enabled: boolean;
  public_url?: string;
  token?: string;
  enabled_at?: string;
}
