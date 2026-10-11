#!/usr/bin/env bash
# CardDAV (RFC 6352) in depth: address-book creation, vCard PUT/GET/DELETE with
# ETag preconditions, UID uniqueness, comma lists, addressbook-query /
# addressbook-multiget, PROPPATCH, REST <-> DAV consistency incl. photos.
set -u
SUITE_DESC_dav_carddav="MKCOL address book, vCard PUT/GET/DELETE + ETags, REPORTs, PROPPATCH"

suite_dav_carddav() {
	ensure_user U1
	user_ensure_dav U1 || die "dav_carddav: no DAV app password"
	mk_addressbook CC_AB "$(uniq carddav)" || die "dav_carddav: cannot create an address book"
	CC_COL="$(dav_home_ab)$CC_AB_PATH/"
	t_run "extended MKCOL creates an address book" t_cc_mkcol
	t_run "MKCOL on an existing address book is refused (405)" t_cc_mkcol_existing
	t_run "PUT with If-None-Match: * creates (201 + ETag)" t_cc_put_create
	t_run "PUT with If-None-Match: * on an existing card is 412" t_cc_put_create_again
	t_run "GET returns the vCard with the same ETag" t_cc_get
	t_run "PUT with matching If-Match updates; stale If-Match is 412" t_cc_if_match
	t_run "same UID at a second path is a 409" t_cc_uid_conflict
	t_run "PUT rejects wrong media type and malformed vCards" t_cc_put_invalid
	t_run "comma lists (CATEGORIES) survive the round trip" t_cc_categories
	t_run "vCard 4.0 is accepted" t_cc_vcard4
	t_run "addressbook-query filters on FN" t_cc_query
	t_run "addressbook-multiget returns requested cards, 404 for missing" t_cc_multiget
	t_run "cards PUT over DAV appear in REST, and vice versa" t_cc_rest_crosscheck
	t_run "a photo uploaded over REST is served in the DAV vCard" t_cc_photo
	t_run "PROPPATCH renames the address book" t_cc_proppatch
	t_run "DELETE with stale If-Match is 412; DELETE removes the card" t_cc_delete
	t_run "DELETE on the collection removes the address book" t_cc_delete_collection
}

_cc_find_ab_by_path() {
	api GET /addressbooks/
	CC_FOUND=$(jq --arg p "$1" '[.addressbooks[] | select(.Path == $p)] | length' <<<"$RESP_BODY")
	CC_FOUND_NAME=$(jq -r --arg p "$1" '[.addressbooks[] | select(.Path == $p)][0].Name // ""' <<<"$RESP_BODY")
}
_cc_cleanup_path() {
	local p=$1 u
	api GET /addressbooks/
	for u in $(jq -r --arg p "$p" '.addressbooks[] | select(.Path == $p) | .UUID' <<<"$RESP_BODY"); do
		defer "delete DAV-created address book $p" cleanup_api U1 DELETE "/addressbooks/$u" --json '{"confirmation":"DELETE"}'
	done
}
CC_MKCOL_BODY='<?xml version="1.0" encoding="utf-8"?>
<d:mkcol xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:set><d:prop><d:resourcetype><d:collection/><c:addressbook/></d:resourcetype><d:displayname>Made by MKCOL</d:displayname></d:prop></d:set></d:mkcol>'

CC_SLUG=''
t_cc_mkcol() {
	CC_SLUG="$(uniq book)"
	dav MKCOL "$(dav_home_ab)$CC_SLUG/" --xml "$CC_MKCOL_BODY"
	assert_status 201 || return
	_cc_cleanup_path "$CC_SLUG"
	_cc_find_ab_by_path "$CC_SLUG"
	assert_eq "$CC_FOUND" 1 "exactly one address book at the new path"
	assert_eq "$CC_FOUND_NAME" "Made by MKCOL" "the displayname from MKCOL must be kept"
	dav PROPFIND "$(dav_home_ab)$CC_SLUG/" --depth 0 --xml "$PROPFIND_COLLECTION"
	assert_status 207 || return
	assert_xpath "$(lp resourcetype addressbook)"
}

t_cc_mkcol_existing() {
	need CC_SLUG || return
	t_known F03
	dav MKCOL "$(dav_home_ab)$CC_SLUG/" --xml "$CC_MKCOL_BODY"
	assert_status 405 "MKCOL on an existing address book (RFC 4918 9.3.1)"
	_cc_find_ab_by_path "$CC_SLUG"
	assert_eq "$CC_FOUND" 1 "no duplicate address book may be created at '$CC_SLUG'"
	if ((CC_FOUND > 1)); then
		local u
		for u in $(jq -r --arg p "$CC_SLUG" '.addressbooks[] | select(.Path == $p) | .UUID' <<<"$RESP_BODY"); do
			defer "delete duplicate address book at $CC_SLUG" cleanup_api U1 DELETE "/addressbooks/$u" --json '{"confirmation":"DELETE"}'
		done
	fi
}

CC_UID='' CC_OBJ='' CC_ETAG=''
t_cc_put_create() {
	CC_UID="$(uniq card1)"
	CC_OBJ="${CC_COL}$CC_UID.vcf"
	dav PUT "$CC_OBJ" --vcf "$(vcard "$CC_UID" "Charlie Davtest" "EMAIL;TYPE=work:charlie@example.test" "TEL;TYPE=cell:+49 30 555")" -H 'If-None-Match: *'
	assert_status 201 || return
	CC_ETAG=$(hdr ETag)
	[[ $CC_ETAG =~ ^\".+\"$ ]] || t_fail "PUT must return a quoted ETag, got '$CC_ETAG'"
}

t_cc_put_create_again() {
	need CC_OBJ || return
	dav PUT "$CC_OBJ" --vcf "$(vcard "$CC_UID" "Clobber Card")" -H 'If-None-Match: *'
	assert_status 412
}

t_cc_get() {
	need CC_OBJ || return
	dav GET "$CC_OBJ"
	assert_status 200 || return
	assert_header Content-Type text/vcard
	assert_header_eq ETag "$CC_ETAG"
	assert_contains "UID:$CC_UID"
	assert_contains "FN:Charlie Davtest"
	assert_contains "charlie@example.test"
}

t_cc_if_match() {
	need CC_ETAG || return
	dav PUT "$CC_OBJ" --vcf "$(vcard "$CC_UID" "Charlie Davtest v2")" -H "If-Match: $CC_ETAG"
	assert_status 200 201 204 || return
	dav GET "$CC_OBJ"
	assert_contains "FN:Charlie Davtest v2"
	local new
	new=$(hdr ETag)
	assert_ne "$new" "$CC_ETAG" "the ETag must change"
	dav PUT "$CC_OBJ" --vcf "$(vcard "$CC_UID" "Stale Write")" -H "If-Match: $CC_ETAG"
	assert_status 412 || return
	dav GET "$CC_OBJ"
	assert_not_contains "Stale Write" "a rejected write must not change the card"
	CC_ETAG=$new
}

t_cc_uid_conflict() {
	need CC_UID || return
	dav PUT "${CC_COL}$(uniq other).vcf" --vcf "$(vcard "$CC_UID" "Same UID")"
	assert_status 409 || return
	dav GET "${CC_COL}$(uniq other).vcf"
	assert_status 404
}

t_cc_put_invalid() {
	dav PUT "${CC_COL}$(uniq bad1).vcf" --data "$(vcard "$(uniq bad1)" "Bad Type")" --ctype application/json
	assert_status_class 4xx "non-vCard media type"
	dav PUT "${CC_COL}$(uniq bad2).vcf" --data 'not a vcard at all' --ctype text/vcard
	assert_status 400 "malformed vCard"
	dav GET "${CC_COL}$(uniq bad2).vcf"
	assert_status 404
}

t_cc_categories() {
	local uid
	uid="$(uniq cats)"
	dav PUT "${CC_COL}$uid.vcf" --vcf "$(vcard "$uid" "Cat Egories" "CATEGORIES:Work,Important")"
	assert_status 201 204 || return
	dav GET "${CC_COL}$uid.vcf"
	assert_status 200 || return
	assert_not_contains 'Work\,Important' "a comma-separated list must not come back escaped into one value"
	# One property with a list, or one property per value — both mean the same.
	[[ $RESP_BODY == *"CATEGORIES:Work,Important"* ||
		($RESP_BODY == *$'CATEGORIES:Work\r'* && $RESP_BODY == *$'CATEGORIES:Important\r'*) ]] ||
		t_fail "categories Work and Important must both survive" with-response
}

t_cc_vcard4() {
	local uid
	uid="$(uniq v4)"
	dav PUT "${CC_COL}$uid.vcf" --vcf $'BEGIN:VCARD\r\nVERSION:4.0\r\nUID:'"$uid"$'\r\nFN:Vera Four\r\nN:Four;Vera;;;\r\nEMAIL;TYPE=home:vera@example.test\r\nEND:VCARD\r\n'
	assert_status 201 204 || return
	dav GET "${CC_COL}$uid.vcf"
	assert_status 200 || return
	assert_contains "FN:Vera Four"
}

t_cc_query() {
	local n
	for n in "Alice Testerson" "Bob Testerson" "Carl Unrelated"; do
		dav PUT "${CC_COL}$(uniq "q-${n%% *}").vcf" --vcf "$(vcard "$(uniq "q-${n%% *}")" "$n")"
		assert_status 201 204 || return
	done
	dav REPORT "$CC_COL" --depth 1 --xml '<?xml version="1.0" encoding="utf-8"?>
<c:addressbook-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav">
<d:prop><d:getetag/><c:address-data/></d:prop>
<c:filter><c:prop-filter name="FN"><c:text-match collation="i;unicode-casemap" match-type="contains">testerson</c:text-match></c:prop-filter></c:filter>
</c:addressbook-query>'
	assert_status 207 || return
	assert_contains "$(uniq q-Alice)"
	assert_contains "$(uniq q-Bob)"
	assert_not_contains "$(uniq q-Carl)" "a non-matching card must not be returned"
	assert_xpath "$(lp response propstat prop address-data)" "address-data must be returned when requested"
}

t_cc_multiget() {
	need CC_OBJ || return
	local missing other
	missing="${CC_COL}$(uniq missing).vcf" other="${CC_COL}$(uniq q-Bob).vcf"
	dav REPORT "$CC_COL" --depth 1 --xml "<?xml version=\"1.0\" encoding=\"utf-8\"?>
<c:addressbook-multiget xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:carddav\">
<d:prop><d:getetag/><c:address-data/></d:prop>
<d:href>$CC_OBJ</d:href><d:href>$other</d:href><d:href>$missing</d:href></c:addressbook-multiget>"
	assert_status 207 || return
	assert_xpath "$(lp response)[$(el href)='$CC_OBJ']//$(el address-data)[contains(.,'Charlie Davtest v2')]"
	assert_xpath "$(lp response)[$(el href)='$other']//$(el address-data)[contains(.,'Bob Testerson')]"
	assert_xpath "$(lp response)[$(el href)='$missing']//$(el status)[contains(.,'404')]" "a missing href is a 404 inside the multistatus"
}

t_cc_rest_crosscheck() {
	need CC_UID || return
	api GET "/addressbooks/$CC_AB_UUID/contacts?limit=200"
	assert_jq "[.Contacts[] | select(.uid == \"$CC_UID\" and .formatted_name == \"Charlie Davtest v2\")] | length == 1" \
		"a card PUT over DAV must appear in REST" || return
	local id uid
	mk_contact id "$CC_AB_UUID" "Rest Made $E2E_RUN_ID" given_name=Rest family_name=Made || {
		t_fail "REST create failed" with-response
		return
	}
	uid=$(jget .uid)
	CC_REST_ID=$id
	dav PROPFIND "$CC_COL" --depth 1 --xml "$PROPFIND_LIST"
	CC_REST_HREF=$(xpath "string(($(lp response href))[contains(.,'$uid')])")
	if [[ -z $CC_REST_HREF ]]; then
		t_fail "the REST-created contact (uid $uid) is not listed over CardDAV" with-response
		return
	fi
	dav GET "$CC_REST_HREF"
	assert_status 200 || return
	assert_contains "FN:Rest Made $E2E_RUN_ID"
}

t_cc_photo() {
	need CC_REST_ID CC_REST_HREF || return
	api PUT "/addressbooks/$CC_AB_UUID/contacts/$CC_REST_ID/photo" --file "$(png_fixture)" --ctype image/png
	assert_status 204 200 || return
	dav GET "$CC_REST_HREF"
	assert_status 200 || return
	assert_match "PHOTO[;:]" "the vCard served over CardDAV must carry the PHOTO"
}

t_cc_proppatch() {
	dav PROPPATCH "$CC_COL" --xml '<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>Book renamed over DAV</d:displayname></d:prop></d:set></d:propertyupdate>'
	assert_status 207 || return
	api GET "/addressbooks/$CC_AB_UUID"
	assert_json .Name "Book renamed over DAV"
}

t_cc_delete() {
	need CC_OBJ || return
	dav DELETE "$CC_OBJ" -H 'If-Match: "stale"'
	assert_status 412 || return
	dav DELETE "$CC_OBJ" -H "If-Match: $CC_ETAG"
	assert_status 204 200 || return
	dav GET "$CC_OBJ"
	assert_status 404
	api GET "/addressbooks/$CC_AB_UUID/contacts?limit=200"
	assert_jq "[.Contacts[] | select(.uid == \"$CC_UID\")] | length == 0" "a DAV delete removes the REST contact"
}

t_cc_delete_collection() {
	local slug
	slug="$(uniq doomed-book)"
	dav MKCOL "$(dav_home_ab)$slug/" --xml "$CC_MKCOL_BODY"
	assert_status 201 || return
	_cc_cleanup_path "$slug"
	dav PUT "$(dav_home_ab)$slug/x.vcf" --vcf "$(vcard "$(uniq doomed-card)" "Doomed Card")"
	dav DELETE "$(dav_home_ab)$slug/"
	assert_status 204 200 || return
	_cc_find_ab_by_path "$slug"
	assert_eq "$CC_FOUND" 0 "the address book must be gone from REST"
	dav PROPFIND "$(dav_home_ab)$slug/" --depth 0 --xml "$PROPFIND_LIST"
	assert_status 404
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	# shellcheck source=lib/e2e.sh
	source "$(dirname "$0")/lib/e2e.sh"
	e2e_suite_main dav_carddav "$@"
fi
