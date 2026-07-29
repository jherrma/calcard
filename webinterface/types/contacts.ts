export interface AddressBook {
  ID: number;
  UUID: string;
  UserID: number;
  Path: string;
  Name: string;
  Description: string;
  CreatedAt: string;
  UpdatedAt: string;
  // Sharing metadata (#53). Note these three are snake_case while the fields
  // above are PascalCase — the backend embeds the raw GORM model and adds these
  // as tagged JSON fields; see the "AddressBook vs Calendar field naming" note
  // in CLAUDE.md. Mirrors what Calendar already exposes.
  /** True when another user shared this book with you (you are not the owner). */
  shared?: boolean;
  /** Your effective access level: 'owner' | 'read-write' | 'read'. */
  permission?: string;
  /** Present only on shared books, so the UI can render "Shared by <name>". */
  owner?: {
    id: string;
    display_name: string;
  };
}

export interface ContactEmail {
  type: string;
  value: string;
  primary?: boolean;
}

export interface ContactPhone {
  type: string;
  value: string;
  primary?: boolean;
}

export interface ContactAddress {
  type: string;
  street?: string;
  city?: string;
  state?: string;
  postal_code?: string;
  country?: string;
}

export interface ContactURL {
  type: string;
  value: string;
}

export interface ContactFormData {
  prefix: string;
  given_name: string;
  middle_name: string;
  family_name: string;
  suffix: string;
  nickname: string;
  organization: string;
  title: string;
  emails: ContactEmail[];
  phones: ContactPhone[];
  addresses: ContactAddress[];
  urls: ContactURL[];
  birthday: string;
  notes: string;
}

export interface Contact {
  id: string;
  addressbook_id: string;
  uid: string;
  etag?: string;
  prefix?: string;
  given_name?: string;
  middle_name?: string;
  family_name?: string;
  suffix?: string;
  nickname?: string;
  formatted_name: string;
  organization?: string;
  title?: string;
  emails?: ContactEmail[];
  phones?: ContactPhone[];
  addresses?: ContactAddress[];
  urls?: ContactURL[];
  birthday?: string;
  notes?: string;
  photo_url?: string;
  created_at: string;
  updated_at: string;
}
