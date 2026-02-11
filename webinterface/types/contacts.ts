export interface AddressBook {
  ID: number;
  UUID: string;
  UserID: number;
  Name: string;
  Description: string;
  CreatedAt: string;
  UpdatedAt: string;
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
