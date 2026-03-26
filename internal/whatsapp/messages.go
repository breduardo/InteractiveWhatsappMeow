package whatsapp

import (
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type MediaMessage struct {
	Message     *waE2E.Message
	MessageType string
	MimeType    string
	FileName    string
}

func NewTextMessage(text string) *waE2E.Message {
	return &waE2E.Message{
		Conversation: proto.String(strings.TrimSpace(text)),
	}
}

func NewReplyMessage(text, chatJID, senderJID, targetMessageID string) *waE2E.Message {
	return &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(strings.TrimSpace(text)),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String(targetMessageID),
				Participant: proto.String(senderJID),
				RemoteJID:   proto.String(chatJID),
			},
		},
	}
}

func BuildMediaMessage(upload whatsmeow.UploadResponse, mimeType, fileName, caption string) (*MediaMessage, error) {
	mimeType = strings.TrimSpace(mimeType)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(fileName))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	base := mediaBaseFromMime(mimeType)
	switch base {
	case "image":
		return &MediaMessage{
			MessageType: "image",
			MimeType:    mimeType,
			FileName:    fileName,
			Message: &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{
					Caption:       proto.String(caption),
					Mimetype:      proto.String(mimeType),
					URL:           proto.String(upload.URL),
					DirectPath:    proto.String(upload.DirectPath),
					MediaKey:      upload.MediaKey,
					FileEncSHA256: upload.FileEncSHA256,
					FileSHA256:    upload.FileSHA256,
					FileLength:    proto.Uint64(upload.FileLength),
				},
			},
		}, nil
	case "video":
		return &MediaMessage{
			MessageType: "video",
			MimeType:    mimeType,
			FileName:    fileName,
			Message: &waE2E.Message{
				VideoMessage: &waE2E.VideoMessage{
					Caption:       proto.String(caption),
					Mimetype:      proto.String(mimeType),
					URL:           proto.String(upload.URL),
					DirectPath:    proto.String(upload.DirectPath),
					MediaKey:      upload.MediaKey,
					FileEncSHA256: upload.FileEncSHA256,
					FileSHA256:    upload.FileSHA256,
					FileLength:    proto.Uint64(upload.FileLength),
				},
			},
		}, nil
	case "audio":
		return &MediaMessage{
			MessageType: "audio",
			MimeType:    mimeType,
			FileName:    fileName,
			Message: &waE2E.Message{
				AudioMessage: &waE2E.AudioMessage{
					Mimetype:      proto.String(mimeType),
					URL:           proto.String(upload.URL),
					DirectPath:    proto.String(upload.DirectPath),
					MediaKey:      upload.MediaKey,
					FileEncSHA256: upload.FileEncSHA256,
					FileSHA256:    upload.FileSHA256,
					FileLength:    proto.Uint64(upload.FileLength),
				},
			},
		}, nil
	default:
		return &MediaMessage{
			MessageType: "document",
			MimeType:    mimeType,
			FileName:    fileName,
			Message: &waE2E.Message{
				DocumentMessage: &waE2E.DocumentMessage{
					Caption:       proto.String(caption),
					Title:         proto.String(fileName),
					FileName:      proto.String(fileName),
					Mimetype:      proto.String(mimeType),
					URL:           proto.String(upload.URL),
					DirectPath:    proto.String(upload.DirectPath),
					MediaKey:      upload.MediaKey,
					FileEncSHA256: upload.FileEncSHA256,
					FileSHA256:    upload.FileSHA256,
					FileLength:    proto.Uint64(upload.FileLength),
				},
			},
		}, nil
	}
}

func ExtractText(message *waE2E.Message) string {
	if message == nil {
		return ""
	}
	switch {
	case message.Conversation != nil:
		return message.GetConversation()
	case message.ExtendedTextMessage != nil:
		return message.GetExtendedTextMessage().GetText()
	case message.ImageMessage != nil:
		return message.GetImageMessage().GetCaption()
	case message.VideoMessage != nil:
		return message.GetVideoMessage().GetCaption()
	case message.DocumentMessage != nil:
		return message.GetDocumentMessage().GetCaption()
	default:
		return ""
	}
}

func DetectMessageType(message *waE2E.Message) string {
	if message == nil {
		return "unknown"
	}
	switch {
	case message.Conversation != nil:
		return "text"
	case message.ExtendedTextMessage != nil:
		return "text"
	case message.ImageMessage != nil:
		return "image"
	case message.VideoMessage != nil:
		return "video"
	case message.AudioMessage != nil:
		return "audio"
	case message.DocumentMessage != nil:
		return "document"
	default:
		return "unknown"
	}
}

func MarshalMessage(message *waE2E.Message) json.RawMessage {
	if message == nil {
		return json.RawMessage(`{}`)
	}

	payload, err := protojson.Marshal(message)
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return payload
}

func MediaMetadata(message *waE2E.Message) (mimeType, fileName string) {
	if message == nil {
		return "", ""
	}
	switch {
	case message.ImageMessage != nil:
		return message.GetImageMessage().GetMimetype(), ""
	case message.VideoMessage != nil:
		return message.GetVideoMessage().GetMimetype(), ""
	case message.AudioMessage != nil:
		return message.GetAudioMessage().GetMimetype(), ""
	case message.DocumentMessage != nil:
		return message.GetDocumentMessage().GetMimetype(), message.GetDocumentMessage().GetFileName()
	default:
		return "", ""
	}
}

func ResolveReceiptStatus(receiptType types.ReceiptType) string {
	switch receiptType {
	case types.ReceiptTypeDelivered:
		return "delivered"
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf, types.ReceiptTypePlayed:
		return "read"
	default:
		return "sent"
	}
}

func DetectMediaTypeFromMime(mimeType string) whatsmeow.MediaType {
	switch mediaBaseFromMime(strings.TrimSpace(mimeType)) {
	case "image":
		return whatsmeow.MediaImage
	case "video":
		return whatsmeow.MediaVideo
	case "audio":
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

func mediaBaseFromMime(mimeType string) string {
	parts := strings.SplitN(mimeType, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func MustParseJID(raw string) (types.JID, error) {
	jid, err := types.ParseJID(raw)
	if err != nil {
		return types.EmptyJID, fmt.Errorf("parse jid %q: %w", raw, err)
	}
	return jid, nil
}
