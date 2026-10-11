"""Remote calendar subscriptions (story 100). URL validation always runs; the
fetch/refresh part needs a feed the SERVER can reach, served from this
machine by a local http.server thread (harness/feed.py). When the server
refuses it (SSRF guard for private hosts, http:// not allowed) or cannot
reach it, those tests are SKIPPED with the reason — never failed."""

import pytest

from harness.data import ics_event
from harness.feed import FeedUnavailable
from harness.xml import lp

DESCRIPTION = "calendar subscriptions: validation, fetch, read-only mirror, refresh"

pytestmark = pytest.mark.usefixtures("u1", "u2")

FEED_V1 = ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nX-WR-CALNAME:E2E test feed\r\n"
           "BEGIN:VEVENT\r\nUID:one@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T100000Z\r\n"
           "DTEND:20300301T110000Z\r\nSUMMARY:Feed event one\r\nEND:VEVENT\r\n"
           "BEGIN:VEVENT\r\nUID:two@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300302T100000Z\r\n"
           "DTEND:20300302T110000Z\r\nSUMMARY:Feed event two\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
FEED_V2 = ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\nX-WR-CALNAME:E2E test feed\r\n"
           "BEGIN:VEVENT\r\nUID:one@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300301T100000Z\r\n"
           "DTEND:20300301T110000Z\r\nSUMMARY:Feed event one (renamed)\r\nEND:VEVENT\r\n"
           "BEGIN:VEVENT\r\nUID:three@feed.e2e\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20300303T100000Z\r\n"
           "DTEND:20300303T110000Z\r\nSUMMARY:Feed event three\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")


@pytest.fixture(scope="module", autouse=True)
def subs_enabled(e2e, u1, u2):
    """DAV credentials for U1, then feature detection. (The bash suite
    reported disabled subscriptions as ONE skipped test, "calendar
    subscriptions"; here every test of the suite is skipped with that reason.)"""
    u1.require_dav()
    r = u1.api("GET", "/calendar-subscriptions/")
    if r.status == 404:
        pytest.skip("the endpoints are not routed (404) — subscriptions disabled on the target "
                    "(CALDAV_SUBSCRIPTIONS_ENABLED=false)")


def _summaries(u1, cal):
    r = u1.api("GET", f"/calendars/{cal}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z&expand=false")
    try:
        return "|".join(sorted(e["summary"] for e in r.json()["events"]))
    except (TypeError, KeyError):
        return ""


def _need(t, st):
    """Skip the running test when the fetch part is unavailable."""
    if getattr(st, "sub_skip", ""):
        t.skip(st.sub_skip)
    t.need(st, "sub_id")


def test_list_empty(t, u1):
    """list starts empty for a new account"""
    r = u1.api("GET", "/calendar-subscriptions/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len(j["subscriptions"]) == 0)


def test_bad_urls(t, e2e, u1):
    """create rejects unusable URLs (scheme, credentials, host)"""
    secret = f"secret-{e2e.run_id}"
    for u in ["ftp://example.com/feed.ics", f"https://user:{secret}@example.com/feed.ics", "https:///feed.ics",
              "not a url", ""]:
        r = u1.api("POST", "/calendar-subscriptions/", json={"url": u})
        t.status(r, 400, msg=f"url '{u}'")
        t.not_contains(r, secret, "a URL's credentials must never be echoed back")
    r = u1.api("GET", "/calendar-subscriptions/")
    t.jq(r, lambda j: len(j["subscriptions"]) == 0, "nothing may have been created")


def test_bad_interval(t, u1):
    """create rejects an invalid refresh interval"""
    r = u1.api("POST", "/calendar-subscriptions/", json='{"url":"https://example.invalid/feed.ics","refresh_interval":"1m"}')
    if not t.status(r, 400):
        return
    t.contains(r, "refresh interval")


def test_create(t, e2e, u1, st):
    """create mirrors a feed into a new subscribed calendar"""
    try:
        feed = e2e.feed()
    except FeedUnavailable as e:
        st.sub_skip = f"no local feed server: {e}"
        t.skip(st.sub_skip)
    st.feed = feed
    st.feed_url = feed.url
    feed.put("feed.ics", FEED_V1)
    url = f"{feed.url}/feed.ics"
    r = u1.api("POST", "/calendar-subscriptions/", json={"url": url, "refresh_interval": "1h"})
    if r.status == 400:
        body = r.json()
        msg = (body.get("message") or "") if isinstance(body, dict) else ""
        if "only https" in msg:
            st.sub_skip = ("server refuses plain-http feeds (CALDAV_SUBSCRIPTIONS_ALLOW_INSECURE_URLS=false); "
                           "the local feed is http://")
        elif "publicly routable" in msg:
            st.sub_skip = ("server refuses private/loopback feed hosts (SSRF guard, "
                           f"CALDAV_SUBSCRIPTIONS_ALLOW_PRIVATE_HOSTS=false); feed at {url}")
        elif feed.hit_count("feed.ics") == 0:
            st.sub_skip = (f"the server could not reach the local feed at {url} ({msg}) — set E2E_FEED_HOST "
                           "to an address of this machine the server can reach")
        if getattr(st, "sub_skip", ""):
            t.skip(st.sub_skip)
    if not t.status(r, 201):
        return
    st.sub_id, st.sub_cal = r.jdict().get("id"), r.jdict().get("calendar_id")
    e2e.defer("delete subscription", e2e.cleanup_api, u1, "DELETE", f"/calendar-subscriptions/{st.sub_id}")
    t.json(r, "name", "E2E test feed", "the calendar is named from X-WR-CALNAME")
    t.json(r, "status", "synced")
    t.json(r, "event_count", 2)
    t.json(r, "refresh_interval", "1h")
    t.nonempty(r, "last_synced_at")
    t.eq(feed.hit_count("feed.ics"), 1, "the feed is fetched once, at create time")


def test_landing_page(t, u1, st):
    """a non-iCalendar URL (landing page) is refused"""
    if getattr(st, "sub_skip", ""):
        t.skip(st.sub_skip)
    t.need(st, "feed_url")
    st.feed.put("landing.html", "<!DOCTYPE html><html><body>Subscribe!</body></html>", "text/html")
    r = u1.api("POST", "/calendar-subscriptions/", json={"url": f"{st.feed_url}/landing.html"})
    if not t.status(r, 400):
        return
    t.contains(r, "iCalendar")


def test_listed(t, u1, st):
    """the mirror is listed as subscribed with the feed's events"""
    _need(t, st)
    r = u1.api("GET", "/calendars/")
    t.jq(r, lambda j: [c for c in j["calendars"] if c["uuid"] == st.sub_cal][0]["subscribed"] is True,
         "the mirror carries subscribed=true")
    t.eq(_summaries(u1, st.sub_cal), "Feed event one|Feed event two")
    r = u1.api("GET", "/calendar-subscriptions/")
    t.jq(r, lambda j: len([s for s in j["subscriptions"] if s["id"] == st.sub_id]) == 1)


def test_readonly_rest(t, e2e, u1, st):
    """the mirror refuses writes over REST"""
    _need(t, st)
    cal = st.sub_cal
    r = u1.api("POST", f"/calendars/{cal}/events",
               json='{"summary":"mine","start":"2030-03-05T10:00:00Z","end":"2030-03-05T11:00:00Z"}')
    t.status(r, 403, msg="REST create")
    r = u1.api("GET", f"/calendars/{cal}/events?start=2030-01-01T00:00:00Z&end=2031-01-01T00:00:00Z")
    try:
        victim = r.json()["events"][0]["id"]
    except (TypeError, KeyError, IndexError):
        victim = None
    t.status(u1.api("PATCH", f"/calendars/{cal}/events/{victim}", json='{"summary":"edited"}'), 403, msg="REST update")
    t.status(u1.api("DELETE", f"/calendars/{cal}/events/{victim}"), 403, msg="REST delete")
    r = u1.api("POST", f"/calendars/{cal}/import",
               data=ics_event(e2e.uniq("imp"), "Imported", "20300305T100000Z", "20300305T110000Z"), ctype="text/calendar")
    t.status_class(r, "4xx", msg="import")
    t.eq(_summaries(u1, cal), "Feed event one|Feed event two", "the mirror must be unchanged")


def test_readonly_dav(t, e2e, u1, st):
    """the mirror refuses writes over CalDAV and exposes CS:source"""
    _need(t, st)
    r = u1.api("GET", f"/calendars/{st.sub_cal}")
    body = r.json()
    col = f"{u1.home_cal}{body.get('path') if isinstance(body, dict) else None}/"
    r = u1.dav("PROPFIND", col, depth=0, xml='<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" '
               'xmlns:cs="http://calendarserver.org/ns/"><d:prop><d:current-user-privilege-set/><cs:source/>'
               '<d:resourcetype/></d:prop></d:propfind>')
    if not t.status(r, 207):
        return
    t.xpath(r, lp("current-user-privilege-set", "**", "read"))
    t.xpath(r, f"not({lp('current-user-privilege-set', '**', 'write')})", "a mirror must not advertise write")
    t.contains(r, f"{st.feed_url}/feed.ics", "CS:source names the feed")
    t.xpath(r, lp("resourcetype", "calendar"), "it stays a calendar for clients")
    r = u1.dav("PUT", f"{col}{e2e.uniq('intruder')}.ics",
               ics=ics_event(e2e.uniq("intruder"), "Intruder", "20300305T100000Z", "20300305T110000Z"))
    t.status(r, 403, msg="DAV PUT")


def test_refresh(t, u1, st):
    """refresh applies the feed's changes (created/updated/deleted)"""
    _need(t, st)
    st.feed.put("feed.ics", FEED_V2)
    r = u1.api("POST", f"/calendar-subscriptions/{st.sub_id}/refresh")
    if not t.status(r, 200):
        return
    t.json(r, "synced", True)
    t.json(r, "created", 1)
    t.json(r, "updated", 1)
    t.json(r, "deleted", 1)
    t.json(r, "status", "synced")
    t.eq(_summaries(u1, st.sub_cal), "Feed event one (renamed)|Feed event three")


def test_patch(t, u1, st):
    """settings (name, colour, interval) can be changed"""
    _need(t, st)
    r = u1.api("PATCH", f"/calendar-subscriptions/{st.sub_id}",
               json='{"name":"Renamed feed","color":"#123456","refresh_interval":"6h"}')
    if not t.status(r, 200):
        return
    t.json(r, "name", "Renamed feed")
    t.json(r, "color", "#123456")
    t.json(r, "refresh_interval", "6h")
    r = u1.api("GET", f"/calendars/{st.sub_cal}")
    t.json(r, "name", "Renamed feed", "the calendar follows the subscription name")
    r = u1.api("PATCH", f"/calendar-subscriptions/{st.sub_id}", json='{"refresh_interval":"15m1s"}')
    t.status(r, 400, msg="intervals are a closed set")


def test_failing_feed(t, u1, st):
    """a failing feed is reported on the subscription (HTTP 200)"""
    _need(t, st)
    st.feed.rm("feed.ics")
    r = u1.api("POST", f"/calendar-subscriptions/{st.sub_id}/refresh")
    if not t.status(r, 200, msg="a feed failure is not a request failure"):
        return
    t.json(r, "status", "error")
    t.jq(r, lambda j: j["error_count"] >= 1)
    t.nonempty(r, "last_error")
    t.jq(r, lambda j: st.feed_url not in j["last_error"],
         "last_error must not repeat the URL (it may carry a secret token)")
    t.eq(_summaries(u1, st.sub_cal), "Feed event one (renamed)|Feed event three", "a failed refresh keeps the last good copy")
    st.feed.put("feed.ics", FEED_V2)


def test_isolation(t, u2, st):
    """another user can neither see nor touch it"""
    _need(t, st)
    r = u2.api("GET", "/calendar-subscriptions/")
    if not t.status(r, 200):
        return
    t.jq(r, lambda j: len(j["subscriptions"]) == 0)
    for m in [f"GET /calendar-subscriptions/{st.sub_id}", f"PATCH /calendar-subscriptions/{st.sub_id}",
              f"DELETE /calendar-subscriptions/{st.sub_id}", f"POST /calendar-subscriptions/{st.sub_id}/refresh"]:
        method, path = m.split(" ", 1)
        t.status(u2.api(method, path, json="{}"), 404, msg=f"U2: {m}")


def test_delete(t, u1, st):
    """delete removes the subscription and its calendar"""
    _need(t, st)
    r = u1.api("DELETE", f"/calendar-subscriptions/{st.sub_id}")
    if not t.status(r, 204):
        return
    t.status(u1.api("GET", f"/calendar-subscriptions/{st.sub_id}"), 404)
    r = u1.api("GET", "/calendars/")
    t.not_contains(r, st.sub_cal, "the mirrored calendar goes with the subscription")
