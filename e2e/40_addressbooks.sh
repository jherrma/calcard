#!/usr/bin/env bash
# Address books and contacts over REST: CRUD (raw JSON, PascalCase address
# books), paging, photos, moving between books, contact search, isolation and
# the "cannot delete your last address book" guard.
set -u
SUITE_DESC_addressbooks="address book + contact CRUD, paging, photo, move, contact search"

suite_addressbooks() {
	ensure_user U1
	ensure_user U2
	t_run "address book create (201, raw JSON)" t_ab_create
	t_run "address book list / get / rename" t_ab_list_get_patch
	t_run "another user cannot read, change or delete it" t_ab_isolation
	t_run "another user's book is a clean 404 (no 5xx / 400)" t_ab_foreign_status
	t_run "contact create returns the parsed contact" t_ab_contact_create
	t_run "contact get / PATCH round trip" t_ab_contact_get_patch
	t_run "contact list pages with limit/offset and sorts by name" t_ab_contact_paging
	t_run "contact photo: upload, fetch, replace, delete" t_ab_photo
	t_run "contact photo: wrong type is 415" t_ab_photo_type
	t_run "unknown contact is a 404 for every verb" t_ab_contact_unknown
	t_run "contact move keeps the UID" t_ab_contact_move
	t_run "contact search finds by name and e-mail" t_ab_search
	t_run "contact search validates its input" t_ab_search_invalid
	t_run "contact delete is 204, then 404" t_ab_contact_delete
	t_run "address book delete needs the confirmation" t_ab_delete
	t_run "the last remaining address book cannot be deleted" t_ab_last_guard
}

AB1_UUID='' AB1_ID='' AB1_PATH='' AB1_NAME=''
t_ab_create() {
	AB1_NAME=$(uniq "Colleagues")
	api POST /addressbooks/ --json "$(jobj name="$AB1_NAME" description="Work contacts")"
	assert_status 201 || return
	AB1_UUID=$(jget .UUID) AB1_ID=$(jget .ID) AB1_PATH=$(jget .Path)
	defer "delete address book $AB1_NAME" cleanup_api U1 DELETE "/addressbooks/$AB1_UUID" --json '{"confirmation":"DELETE"}'
	assert_json .Name "$AB1_NAME"
	assert_json .Description "Work contacts"
	assert_jq '.ID > 0 and (.UUID | test("^[0-9a-f-]{36}$"))'
	assert_json_nonempty .Path
	api POST /addressbooks/ --json '{"description":"no name"}'
	assert_status 400 "an address book needs a name"
}

t_ab_list_get_patch() {
	need AB1_UUID || return
	api GET /addressbooks/
	assert_status 200 || return
	assert_jq "[.addressbooks[] | select(.UUID == \"$AB1_UUID\" and .shared == false and .permission == \"owner\")] | length == 1"
	assert_jq '[.addressbooks[] | select(.Name == "Contacts")] | length >= 1' "registration seeds a Contacts book"
	api GET "/addressbooks/$AB1_UUID"
	assert_status 200 || return
	assert_json .UUID "$AB1_UUID"
	AB1_NAME="$AB1_NAME (renamed)"
	api PATCH "/addressbooks/$AB1_UUID" --json "$(jobj name="$AB1_NAME")"
	assert_status 200 || return
	assert_json .Name "$AB1_NAME"
	api GET "/addressbooks/$AB1_UUID"
	assert_json .Name "$AB1_NAME" "rename must persist"
	api GET /addressbooks/00000000-0000-0000-0000-000000000000
	assert_status 404
}

t_ab_isolation() {
	need AB1_UUID || return
	as U2 api GET "/addressbooks/$AB1_UUID"
	assert_status 404 "U2 reading U1's address book"
	as U2 api PATCH "/addressbooks/$AB1_UUID" --json '{"name":"hijacked"}'
	assert_status_not 200 204 "U2 renaming U1's address book"
	as U2 api DELETE "/addressbooks/$AB1_UUID" --json '{"confirmation":"DELETE"}'
	assert_status_not 200 204 "U2 deleting U1's address book"
	as U2 api GET "/addressbooks/$AB1_UUID/contacts"
	assert_status 404 "U2 listing U1's contacts"
	as U2 api POST "/addressbooks/$AB1_UUID/contacts" --json '{"formatted_name":"Intruder"}'
	assert_status 404 "U2 creating a contact in U1's book"
	as U2 api GET /addressbooks/
	assert_not_contains "$AB1_UUID"
	api GET "/addressbooks/$AB1_UUID"
	assert_status 200 "U1's book must survive" || return
	assert_json .Name "$AB1_NAME" "U1's book must be unchanged"
}

t_ab_foreign_status() {
	need AB1_UUID || return
	# The access is refused (see the isolation test); this pins the status:
	# a foreign book is "not found" like on GET — never a 5xx, never a 400.
	t_known F05
	as U2 api PATCH "/addressbooks/$AB1_UUID" --json '{"name":"hijacked"}'
	assert_status 404 "PATCH on another user's book"
	as U2 api DELETE "/addressbooks/$AB1_UUID" --json '{"confirmation":"DELETE"}'
	assert_status 404 "DELETE on another user's book"
	as U2 api GET "/addressbooks/$AB1_UUID/export"
	assert_status 404 "export of another user's book"
}

CT_ID='' CT_UID=''
t_ab_contact_create() {
	need AB1_UUID || return
	api POST "/addressbooks/$AB1_UUID/contacts" --json "$(jobj formatted_name="Jane Doe $E2E_RUN_ID" given_name=Jane family_name="Doe$E2E_RUN_ID" \
		organization="Example Corp" title=Engineer birthday=1990-04-01 notes="met at e2e" \
		'emails:=[{"type":"work","value":"jane@example.test","primary":true}]' \
		'phones:=[{"type":"cell","value":"+49 30 1234567"}]' \
		'addresses:=[{"type":"work","street":"Main St 1","city":"Berlin","postal_code":"10115","country":"DE"}]' \
		'urls:=[{"type":"work","value":"https://example.test"}]')"
	assert_status 201 || return
	CT_ID=$(jget .id) CT_UID=$(jget .uid)
	assert_json .formatted_name "Jane Doe $E2E_RUN_ID"
	assert_json .given_name Jane
	assert_json .organization "Example Corp"
	assert_json '.emails[0].value' jane@example.test
	assert_json '.phones[0].value' "+49 30 1234567"
	assert_json '.addresses[0].city' Berlin
	assert_json .birthday 1990-04-01
	assert_json_nonempty .uid
}

t_ab_contact_get_patch() {
	need CT_ID || return
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID"
	assert_status 200 || return
	assert_json .id "$CT_ID"
	assert_json .title Engineer
	api PATCH "/addressbooks/$AB1_UUID/contacts/$CT_ID" --json "$(jobj formatted_name="Jane Q. Doe $E2E_RUN_ID" given_name=Jane middle_name=Q. family_name="Doe$E2E_RUN_ID")"
	assert_status 200 || return
	assert_json .formatted_name "Jane Q. Doe $E2E_RUN_ID"
	assert_json .middle_name Q.
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID"
	assert_json .formatted_name "Jane Q. Doe $E2E_RUN_ID" "the update must persist"
	assert_json .uid "$CT_UID" "the UID never changes"
}

t_ab_contact_paging() {
	mk_addressbook PG "$(uniq paging)" || {
		t_fail "create failed" with-response
		return
	}
	local n id
	for n in Charlie Alice Bob; do
		mk_contact id "$PG_UUID" "$n Pager" given_name="$n" family_name=Pager || t_fail "seed $n failed" with-response
	done
	api GET "/addressbooks/$PG_UUID/contacts?limit=2&offset=0"
	assert_status 200 || return
	assert_json .Total 3
	assert_json .Limit 2
	assert_jq '.Contacts | length == 2'
	assert_jq '[.Contacts[].formatted_name] == ["Alice Pager", "Bob Pager"]' "default sort is by name, ascending"
	api GET "/addressbooks/$PG_UUID/contacts?limit=2&offset=2"
	assert_jq '[.Contacts[].formatted_name] == ["Charlie Pager"]' "the second page holds the rest"
	api GET "/addressbooks/$PG_UUID/contacts?order=desc"
	assert_jq '.Contacts[0].formatted_name == "Charlie Pager"' "order=desc reverses"
}

t_ab_photo() {
	need CT_ID || return
	local png
	png=$(png_fixture)
	api PUT "/addressbooks/$AB1_UUID/contacts/$CT_ID/photo" --file "$png" --ctype image/png
	assert_status 204 200 || return
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID/photo"
	assert_status 200 || return
	assert_header Content-Type image/png
	cmp -s "$RESP_BODY_FILE" "$png" || t_fail "the photo bytes must round-trip unchanged"
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID"
	assert_json_nonempty .photo_url "the contact must advertise its photo URL"
	api DELETE "/addressbooks/$AB1_UUID/contacts/$CT_ID/photo"
	assert_status 204 200 || return
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID/photo"
	assert_status 404 "a deleted photo is gone"
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID"
	assert_jq '(.photo_url // "") == ""' "photo_url must be cleared"
}

t_ab_photo_type() {
	need CT_ID || return
	api PUT "/addressbooks/$AB1_UUID/contacts/$CT_ID/photo" --data "this is not an image" --ctype image/png
	assert_status 415
}

t_ab_contact_unknown() {
	need AB1_UUID || return
	t_known F05
	local zero=00000000-0000-0000-0000-000000000000
	api GET "/addressbooks/$AB1_UUID/contacts/$zero"
	assert_status 404 "GET"
	api PATCH "/addressbooks/$AB1_UUID/contacts/$zero" --json '{"formatted_name":"x"}'
	assert_status 404 "PATCH"
	api DELETE "/addressbooks/$AB1_UUID/contacts/$zero"
	assert_status 404 "DELETE (must not be a 5xx)"
	api PUT "/addressbooks/$AB1_UUID/contacts/$zero/photo" --file "$(png_fixture)" --ctype image/png
	assert_status 404 "photo upload (must not be a 5xx)"
}

t_ab_contact_move() {
	need AB1_UUID || return
	mk_addressbook MV "$(uniq move-target)" || {
		t_fail "create failed" with-response
		return
	}
	local id uid
	mk_contact id "$AB1_UUID" "Mover McMoveson $E2E_RUN_ID" || {
		t_fail "seed failed" with-response
		return
	}
	uid=$(jget .uid)
	api POST "/addressbooks/$AB1_UUID/contacts/$id/move" --json "$(jobj target_addressbook_id="$MV_UUID")"
	assert_status 200 || return
	assert_json .uid "$uid" "the UID survives the move"
	api GET "/addressbooks/$AB1_UUID/contacts?limit=200"
	assert_jq "[.Contacts[] | select(.uid == \"$uid\")] | length == 0" "gone from the source book"
	api GET "/addressbooks/$MV_UUID/contacts?limit=200"
	assert_jq "[.Contacts[] | select(.uid == \"$uid\")] | length == 1" "present in the target book"
	as U2 api GET /addressbooks/
	local u2ab
	u2ab=$(jget '.addressbooks[0].UUID')
	api POST "/addressbooks/$MV_UUID/contacts/$id/move" --json "$(jobj target_addressbook_id="$u2ab")"
	assert_status_class 4xx "moving into another user's book must be refused"
}

t_ab_search() {
	need CT_ID || return
	api GET "/contacts/search?q=Doe$E2E_RUN_ID"
	assert_status 200 || return
	assert_jq "[.contacts[] | select(.id == \"$CT_ID\")] | length == 1" "search by family name"
	assert_jq '.count == (.contacts | length)'
	api GET "/contacts/search?q=jane@example.test"
	assert_jq "[.contacts[] | select(.id == \"$CT_ID\")] | length == 1" "search by e-mail"
	api GET "/contacts/search?q=Doe$E2E_RUN_ID&addressbook_id=$AB1_UUID"
	assert_jq '.contacts | length == 1' "narrowed to one book"
	as U2 api GET "/contacts/search?q=Doe$E2E_RUN_ID"
	assert_status 200
	assert_jq '.contacts | length == 0' "another user must not find U1's contacts"
}

t_ab_search_invalid() {
	api GET /contacts/search
	assert_status 400 "q is required"
	api GET "/contacts/search?q=x&addressbook_id=00000000-0000-0000-0000-000000000000"
	assert_status 404 "an unknown address book is a 404, not an empty result"
	need AB1_UUID || return
	as U2 api GET "/contacts/search?q=x&addressbook_id=$AB1_UUID"
	assert_status 404 "another user's address book is a 404"
	api_anon GET "/contacts/search?q=x"
	assert_status 401
}

t_ab_contact_delete() {
	need CT_ID || return
	api DELETE "/addressbooks/$AB1_UUID/contacts/$CT_ID"
	assert_status 204 || return
	api GET "/addressbooks/$AB1_UUID/contacts/$CT_ID"
	assert_status 404
	api GET "/contacts/search?q=Doe$E2E_RUN_ID"
	assert_jq '.contacts | length == 0' "a deleted contact must not be found"
}

t_ab_delete() {
	mk_addressbook DELAB "$(uniq delete-me)" || {
		t_fail "create failed" with-response
		return
	}
	api DELETE "/addressbooks/$DELAB_UUID"
	assert_status 400 "missing confirmation"
	api GET "/addressbooks/$DELAB_UUID"
	assert_status 200 "must survive the refused delete" || return
	api DELETE "/addressbooks/$DELAB_UUID" --json '{"confirmation":"DELETE"}'
	assert_status 204 || return
	api GET "/addressbooks/$DELAB_UUID"
	assert_status 404
}

t_ab_last_guard() {
	user_create UAB "E2E Single Book" || {
		t_fail "could not create a scratch user" with-response
		return
	}
	as UAB api GET /addressbooks/
	assert_status 200 || return
	local n uuid
	n=$(jget '.addressbooks | length')
	uuid=$(jget '.addressbooks[0].UUID')
	assert_eq "$n" 1 "a fresh account has exactly one address book" || return
	as UAB api DELETE "/addressbooks/$uuid" --json '{"confirmation":"DELETE"}'
	assert_status_class 4xx "deleting the only address book must be refused"
	as UAB api GET "/addressbooks/$uuid"
	assert_status 200 "the only address book must survive"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main addressbooks "$@"
fi
