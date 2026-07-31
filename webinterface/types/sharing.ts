/**
 * Sharing types (story 043).
 *
 * Calendars and address books expose an IDENTICAL /shares sub-resource — same
 * request body, same response fields — which is what lets a single store and a
 * single panel serve both kinds. Both collections are keyed on the resource
 * UUID (#52), never the numeric id.
 */

export type ShareResourceType = 'calendar' | 'addressbook';

/** The only two grants the backend accepts (it 400s on anything else). */
export type SharePermission = 'read' | 'read-write';

export interface ShareUser {
  id: string;
  username: string;
  display_name: string;
  email: string;
}

/**
 * A single grant. The calendar and address-book endpoints each also return the
 * parent resource's UUID (`calendar_id` / `addressbook_id`), which the UI never
 * needs — it always knows which resource it is looking at — so this one type
 * covers both.
 */
export interface Share {
  id: string;
  shared_with: ShareUser;
  permission: string;
  created_at: string;
}

/** GET /shares response envelope. Raw JSON — NOT the { status, data } wrapper. */
export interface ShareListResponse {
  shares: Share[];
}

export interface PublicAccessStatus {
  enabled: boolean;
  public_url?: string | null;
  token?: string;
  enabled_at?: string;
  /** Only POST …/public/regenerate sets this ("Previous public URL is no longer valid"). */
  message?: string;
}

/**
 * A row of the read-only "Shared with me" overview. There is no
 * /shared-with-me endpoint: this is derived from the calendar and address-book
 * LIST endpoints, which already carry `shared` / `permission` / `owner` (#53).
 */
export interface SharedResource {
  type: ShareResourceType;
  /** Resource UUID — used for keying and for links, never for the share API. */
  uuid: string;
  name: string;
  /** Calendars only. */
  color?: string;
  permission: string;
  ownerName: string;
}
