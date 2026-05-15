package s3

import "encoding/xml"

const xmlNamespace = "http://s3.amazonaws.com/doc/2006-03-01/"

type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type BucketEntry struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

type ListAllMyBucketsResult struct {
	XMLName xml.Name      `xml:"ListAllMyBucketsResult"`
	Xmlns   string        `xml:"xmlns,attr"`
	Owner   Owner         `xml:"Owner"`
	Buckets []BucketEntry `xml:"Buckets>Bucket"`
}

type Contents struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type ListBucketResultV2 struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	Xmlns                 string         `xml:"xmlns,attr"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix"`
	Delimiter             string         `xml:"Delimiter,omitempty"`
	KeyCount              int            `xml:"KeyCount"`
	MaxKeys               int            `xml:"MaxKeys"`
	IsTruncated           bool           `xml:"IsTruncated"`
	ContinuationToken     string         `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	StartAfter            string         `xml:"StartAfter,omitempty"`
	Contents              []Contents     `xml:"Contents"`
	CommonPrefixes        []CommonPrefix `xml:"CommonPrefixes"`
}

type DeleteRequestObject struct {
	Key       string `xml:"Key"`
	VersionID string `xml:"VersionId,omitempty"`
}

type DeleteRequest struct {
	XMLName xml.Name              `xml:"Delete"`
	Objects []DeleteRequestObject `xml:"Object"`
	Quiet   bool                  `xml:"Quiet,omitempty"`
}

type DeletedObject struct {
	Key                   string `xml:"Key"`
	VersionID             string `xml:"VersionId,omitempty"`
	DeleteMarker          bool   `xml:"DeleteMarker,omitempty"`
	DeleteMarkerVersionID string `xml:"DeleteMarkerVersionId,omitempty"`
}

type DeleteErrorObject struct {
	Key     string `xml:"Key"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

type DeleteResult struct {
	XMLName xml.Name            `xml:"DeleteResult"`
	Xmlns   string              `xml:"xmlns,attr"`
	Deleted []DeletedObject     `xml:"Deleted,omitempty"`
	Errors  []DeleteErrorObject `xml:"Error,omitempty"`
}

type ErrorXML struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
	BucketName string  `xml:"BucketName,omitempty"`
	Key        string  `xml:"Key,omitempty"`
}
