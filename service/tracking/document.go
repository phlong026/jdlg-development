package tracking

import (
	"bytes"
	"encoding/json"
	"io"
)

type documentBody struct {
	Schema  int    `json:"schema"`
	MediaID string `json:"media_id"`
}

type documentArchiveRecord struct {
	ContentID string `json:"content_id"`
	MediaID   string `json:"media_id"`
}

func documentArchiveIdentity(draft Draft) (string, documentArchiveRecord, error) {
	_, mediaID, err := normalizeDocumentBody(draft.Body)
	if err != nil {
		return "", documentArchiveRecord{}, err
	}
	record := documentArchiveRecord{MediaID: mediaID}
	return hash("document-archive:" + draft.Owner + ":" + mediaID), record, nil
}

func normalizeDocumentBody(body string) (string, string, error) {
	var document documentBody
	decoder := json.NewDecoder(bytes.NewBufferString(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || document.Schema != 1 || !validID(document.MediaID) {
		return "", "", fail(400, "文档资料格式无效")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", "", fail(400, "文档资料格式无效")
	}
	normalized, err := json.Marshal(document)
	if err != nil {
		return "", "", ErrUnavailable
	}
	return string(normalized), document.MediaID, nil
}

func supportedDocumentAsset(asset Asset) bool {
	// B4：PPTX 纳入文档资料；上传层已完成同源 OOXML 结构与宏校验。
	return asset.Kind == "document" && (asset.MIME == "application/pdf" ||
		asset.MIME == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		asset.MIME == "application/vnd.openxmlformats-officedocument.presentationml.presentation")
}
