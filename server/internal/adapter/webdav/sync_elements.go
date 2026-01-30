package webdav

import (
	"encoding/xml"
)

// SyncCollectionQuery represents the DAV:sync-collection REPORT request
// https://tools.ietf.org/html/rfc6578#section-6.1
type SyncCollectionQuery struct {
	XMLName   xml.Name `xml:"DAV: sync-collection"`
	SyncToken string   `xml:"sync-token"`
	SyncLevel string   `xml:"sync-level"`
	Limit     *Limit   `xml:"limit,omitempty"`
	Prop      *Prop    `xml:"prop"`
}

// Limit represents the DAV:limit element
type Limit struct {
	NResults uint `xml:"nresults"`
}

// Prop represents the DAV:prop element
type Prop struct {
	Raw []RawXMLValue `xml:",any"`
}

// RawXMLValue represents an unparsed XML element
type RawXMLValue struct {
	XMLName xml.Name
	Inner   []byte `xml:",innerxml"`
}

// SyncResponse represents part of the MultiStatus response for sync
type SyncResponse struct {
	XMLName  xml.Name   `xml:"DAV: response"`
	Href     string     `xml:"href"`
	PropStat []PropStat `xml:"propstat,omitempty"`
	Status   string     `xml:"status,omitempty"`
}

type PropStat struct {
	Prop   Prop   `xml:"prop"`
	Status string `xml:"status"`
}
