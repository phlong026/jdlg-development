package route

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

const (
	sharedVideoPlaybackTTL        = 10 * time.Minute
	sharedVideoPlaybackMaxTickets = 4096
)

var errSharedVideoPlaybackFull = errors.New("shared video playback ticket capacity reached")

type sharedVideoKind string

const (
	sharedVideoNote      sharedVideoKind = "note"
	sharedVideoMember    sharedVideoKind = "member"
	sharedVideoPackage   sharedVideoKind = "package"
	sharedVideoPublished sharedVideoKind = "published"
)

type sharedVideoPlaybackGrant struct {
	Kind       sharedVideoKind
	ProjectID  string
	ShareToken string
	Actor      int64
	VersionID  string
	AssetID    string
	ExpiresAt  time.Time
}

var sharedVideoPlaybackGrants = struct {
	sync.Mutex
	items map[[sha256.Size]byte]sharedVideoPlaybackGrant
}{items: map[[sha256.Size]byte]sharedVideoPlaybackGrant{}}

func issueSharedVideoPlayback(grant sharedVideoPlaybackGrant) (string, time.Time, error) {
	expiresAt := time.Now().Add(sharedVideoPlaybackTTL).UTC()
	path, err := issueSharedVideoPlaybackAt(grant, expiresAt)
	return path, expiresAt, err
}

func issueSharedVideoPlaybackAt(grant sharedVideoPlaybackGrant, expiresAt time.Time) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(raw[:])
	grant.ExpiresAt = expiresAt.UTC()
	key := sha256.Sum256([]byte(ticket))
	now := time.Now()
	sharedVideoPlaybackGrants.Lock()
	for existingKey, existing := range sharedVideoPlaybackGrants.items {
		if !existing.ExpiresAt.After(now) {
			delete(sharedVideoPlaybackGrants.items, existingKey)
		}
	}
	if len(sharedVideoPlaybackGrants.items) >= sharedVideoPlaybackMaxTickets {
		sharedVideoPlaybackGrants.Unlock()
		return "", errSharedVideoPlaybackFull
	}
	sharedVideoPlaybackGrants.items[key] = grant
	sharedVideoPlaybackGrants.Unlock()
	expires := grant.ExpiresAt.Unix()
	path := "/api/v1/shared-video/" + ticket + "?exp=" + strconv.FormatInt(expires, 10) + "&sig=" + trackingMediaSignature(ticket, expires)
	return path, nil
}

func sharedVideoPlaybackGrantFor(ticket, expiresRaw, signature string) (sharedVideoPlaybackGrant, bool) {
	if !validTrackingMediaID(ticket) || !trackingMediaSignatureValid(ticket, expiresRaw, signature) {
		return sharedVideoPlaybackGrant{}, false
	}
	expires, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil {
		return sharedVideoPlaybackGrant{}, false
	}
	key := sha256.Sum256([]byte(ticket))
	sharedVideoPlaybackGrants.Lock()
	grant, ok := sharedVideoPlaybackGrants.items[key]
	if !ok || grant.ExpiresAt.Unix() != expires || !grant.ExpiresAt.After(time.Now()) {
		delete(sharedVideoPlaybackGrants.items, key)
		ok = false
	}
	sharedVideoPlaybackGrants.Unlock()
	return grant, ok
}

func registerSharedVideoPlaybackRoutes(v1 *gin.RouterGroup, svc *tracking.Service, verifyActor func(context.Context, int64, sharedVideoKind) error) {
	v1.Match([]string{http.MethodGet, http.MethodHead}, "/shared-video/:ticket", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if verifyActor == nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		grant, ok := sharedVideoPlaybackGrantFor(c.Param("ticket"), c.Query("exp"), c.Query("sig"))
		if !ok {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "播放凭证无效或已过期"})
			return
		}
		switch grant.Kind {
		case sharedVideoNote, sharedVideoPackage, sharedVideoMember:
			if !projectNotesEnabled() {
				trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "私人项目笔记暂未开通"})
				return
			}
		case sharedVideoPublished:
			if !svc.Config.ContentEnabled || !svc.Config.VideoEnabled {
				trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "内容视频暂未开通"})
				return
			}
		default:
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "播放凭证无效或已过期"})
			return
		}
		asset, err := func() (tracking.Asset, error) {
			validationContext, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			if err := verifyActor(validationContext, grant.Actor, grant.Kind); err != nil {
				return tracking.Asset{}, err
			}
			switch grant.Kind {
			case sharedVideoMember:
				return svc.ProjectNoteMemberMedia(validationContext, grant.Actor, grant.ProjectID, grant.VersionID, grant.AssetID)
			case sharedVideoNote:
				return svc.ProjectNoteReadShareMedia(validationContext, grant.ShareToken, grant.Actor, grant.AssetID)
			case sharedVideoPackage:
				return svc.ProjectPackageReadVersionMedia(validationContext, grant.ShareToken, grant.Actor, grant.VersionID, grant.AssetID)
			case sharedVideoPublished:
				return svc.Media(validationContext, nil, grant.AssetID)
			default:
				return tracking.Asset{}, &tracking.Error{Status: http.StatusForbidden, Message: "播放凭证无效或已过期"}
			}
		}()
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		if asset.ID != grant.AssetID || asset.Kind != "video" || asset.MIME != "video/mp4" {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusNotFound, Message: "视频不可访问"})
			return
		}
		serveContentMediaBytes(c, svc, asset)
	})
}

func sharedVideoPlaybackResponse(c *gin.Context, grant sharedVideoPlaybackGrant) {
	path, expiresAt, err := issueSharedVideoPlayback(grant)
	if err != nil {
		if errors.Is(err, errSharedVideoPlaybackFull) {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "播放请求较多，请稍后重试"})
			return
		}
		trackingResponse(c, nil, tracking.ErrUnavailable)
		return
	}
	// The opaque URL contains only a short-lived bearer ticket. Share and account
	// credentials remain server-side and are revalidated on every media request.
	trackingResponse(c, gin.H{"url": path, "expires_at": expiresAt}, nil)
}

func requireSharedVideo(asset tracking.Asset) error {
	if asset.Kind != "video" || asset.MIME != "video/mp4" {
		return &tracking.Error{Status: http.StatusNotFound, Message: "视频不可访问"}
	}
	return nil
}
