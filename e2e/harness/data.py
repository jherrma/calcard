"""Request bodies: iCalendar / vCard builders (CRLF line endings, as RFC 5545 /
6350 require), WebDAV XML bodies and a tiny PNG."""

from __future__ import annotations

import base64

CRLF = "\r\n"


def ics_vevent(uid: str, summary: str, start: str, end: str, *extra: str) -> str:
    """A bare VEVENT block. DTSTART/DTEND in UTC basic format, e.g. 20300601T100000Z."""
    lines = ["BEGIN:VEVENT", f"UID:{uid}", "DTSTAMP:20260101T000000Z", f"DTSTART:{start}",
             f"DTEND:{end}", f"SUMMARY:{summary}", *extra, "END:VEVENT"]
    return CRLF.join(lines) + CRLF


def ics_calendar(*components: str) -> str:
    """Wrap component blocks in a VCALENDAR."""
    return ("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard e2e//EN\r\n"
            + "".join(components) + "END:VCALENDAR\r\n")


def ics_event(uid: str, summary: str, start: str, end: str, *extra: str) -> str:
    """A VCALENDAR with one VEVENT (EXTRA: additional property lines)."""
    return ics_calendar(ics_vevent(uid, summary, start, end, *extra))


def ics_todo(uid: str, summary: str) -> str:
    return ics_calendar(
        f"BEGIN:VTODO\r\nUID:{uid}\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:{summary}\r\n"
        "STATUS:NEEDS-ACTION\r\nEND:VTODO\r\n"
    )


def vcard(uid: str, fn: str, *extra: str) -> str:
    """A vCard 3.0. N is derived from FN: first word = given, rest = family."""
    given, _, family = fn.partition(" ")
    family = family or fn
    lines = ["BEGIN:VCARD", "VERSION:3.0", f"UID:{uid}", f"FN:{fn}", f"N:{family};{given};;;", *extra, "END:VCARD"]
    return CRLF.join(lines) + CRLF


# 1x1 transparent PNG.
PNG_PIXEL = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
)

PROPFIND_PRINCIPAL = """<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:a="urn:ietf:params:xml:ns:carddav">
  <d:prop><d:current-user-principal/><d:resourcetype/><c:calendar-home-set/><a:addressbook-home-set/></d:prop>
</d:propfind>"""

PROPFIND_COLLECTION = """<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:prop><d:resourcetype/><d:displayname/><d:sync-token/><cs:getctag/><d:supported-report-set/><d:current-user-privilege-set/></d:prop>
</d:propfind>"""

PROPFIND_LIST = """<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:displayname/><d:getetag/></d:prop></d:propfind>"""


def sync_body(token: str = "", extra: str = "") -> str:
    """WebDAV-Sync REPORT body (RFC 6578)."""
    return ('<?xml version="1.0" encoding="utf-8"?>\n<d:sync-collection xmlns:d="DAV:">'
            f"<d:sync-token>{token}</d:sync-token><d:sync-level>1</d:sync-level>{extra}"
            "<d:prop><d:getetag/></d:prop></d:sync-collection>")


def proppatch_displayname(name: str) -> str:
    return ('<?xml version="1.0" encoding="utf-8"?>\n<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop>'
            f"<d:displayname>{name}</d:displayname></d:prop></d:set></d:propertyupdate>")
