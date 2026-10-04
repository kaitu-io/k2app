package center

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// 客服会话的图片消息（第 1b 期）。
//
// 存储：私有 S3 桶（viper "chat.images.bucket"，未配置 = 图片功能关闭），对象不公开。
// 消息行 content 存对象 key；对外一律给 Center 签名的查看链接 /api/chat/images/<令牌>，
// 访问时校验令牌、现签一个短期 S3 下载地址并 302 过去。令牌只绑定消息 id 与过期时间，
// 持有链接即可查看（与 Slack 频道、邮件里的链接同一种能力 URL），所以有效期按用途分开给。
// AI 拿的是直接的 S3 临时地址（OpenAI 自己下载，不经 Center）。
// 留存靠桶的生命周期规则，不在这里删。

const (
	chatImagePurpose = "chat-image-v1"

	chatImageMaxBytes = 5 << 20 // 单张上限 5 MB
	// chatImageMaxBody 上传请求体上限：图片本身 + multipart 头与 clientId 的余量
	chatImageMaxBody = chatImageMaxBytes + 64<<10

	chatImageVisitorTTL = 24 * time.Hour      // 访客挂件里的链接（页面开着时会一直用旧链接）
	chatImageSlackTTL   = 30 * 24 * time.Hour // Slack 频道里的链接
	chatImageS3TTL      = 5 * time.Minute     // 跳转出去的 S3 下载地址
	chatImageAITTL      = 15 * time.Minute    // 交给 OpenAI 下载的地址
)

// chatImageTypes 允许的图片类型（按内容嗅探，不信任文件名与客户端声明的类型）→ 对象扩展名。
var chatImageTypes = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// chatImageStore 图片存储；测试里替换成内存实现。
type chatImageStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// chatImages 返回当前的图片存储；nil = 未配置桶（图片功能关闭）。变量以便测试注入。
var chatImages = func() chatImageStore {
	bucket := strings.TrimSpace(viper.GetString("chat.images.bucket"))
	if bucket == "" {
		return nil
	}
	return chatS3Store(bucket)
}

// chatImagesEnabled 图片上传是否可用（下发给挂件，决定画不画"发图片"按钮）。
func chatImagesEnabled() bool { return chatImages() != nil }

var (
	chatS3Mu      sync.Mutex
	chatS3Clients = map[string]*s3ImageStore{}
)

// chatS3Store 按桶名缓存 S3 客户端。凭证：aws.use_imds=true 走实例角色（生产），
// 否则用 aws.access_key + aws.secret_key（或 aws.secret）。区域取 chat.images.region，缺省 aws.region。
func chatS3Store(bucket string) chatImageStore {
	chatS3Mu.Lock()
	defer chatS3Mu.Unlock()
	if s, ok := chatS3Clients[bucket]; ok {
		return s
	}
	s := &s3ImageStore{bucket: bucket}
	chatS3Clients[bucket] = s
	return s
}

type s3ImageStore struct {
	bucket string
	once   sync.Once
	client *s3.Client
	err    error
}

func (s *s3ImageStore) init(ctx context.Context) (*s3.Client, error) {
	s.once.Do(func() {
		region := viper.GetString("chat.images.region")
		if region == "" {
			region = viper.GetString("aws.region")
		}
		opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
		if !viper.GetBool("aws.use_imds") {
			secret := viper.GetString("aws.secret_key")
			if secret == "" {
				secret = viper.GetString("aws.secret")
			}
			key := viper.GetString("aws.access_key")
			if key == "" || secret == "" {
				s.err = fmt.Errorf("chat images: aws credentials not configured")
				return
			}
			opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, "")))
		}
		cfg, err := awsconfig.LoadDefaultConfig(context.WithoutCancel(ctx), opts...)
		if err != nil {
			s.err = fmt.Errorf("chat images: load aws config: %w", err)
			return
		}
		s.client = s3.NewFromConfig(cfg)
	})
	return s.client, s.err
}

func (s *s3ImageStore) Put(ctx context.Context, key, contentType string, data []byte) error {
	c, err := s.init(ctx)
	if err != nil {
		return err
	}
	_, err = c.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(contentType),
		// 浏览器直接打开时也只当图片显示，不当成页面
		ContentDisposition: aws.String("inline"),
	})
	return err
}

func (s *s3ImageStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	c, err := s.init(ctx)
	if err != nil {
		return "", err
	}
	req, err := s3.NewPresignClient(c).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, func(o *s3.PresignOptions) { o.Expires = ttl })
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// chatSniffImage 按内容判断图片类型；不是允许的类型返回 ""。
func chatSniffImage(data []byte) string {
	ct := http.DetectContentType(data)
	if _, ok := chatImageTypes[ct]; ok {
		return ct
	}
	return ""
}

// chatImageKey 新对象的 key：chat/<品牌>/<年月>/<随机>.<扩展名>。不含访客信息，不可猜。
func chatImageKey(b Brand, contentType string, now time.Time) string {
	return fmt.Sprintf("chat/%s/%s/%s.%s", b, now.UTC().Format("200601"), uuid.NewString(), chatImageTypes[contentType])
}

// signChatImageToken 签发查看链接令牌（空串 = jwt.secret 未配置）。
func signChatImageToken(msgID uint64, ttl time.Duration) string {
	return signChatToken(chatImagePurpose, chatTokenClaims{I: msgID}, ttl)
}

func parseChatImageToken(tok string, now time.Time) (uint64, error) {
	c, err := parseChatToken(chatImagePurpose, tok, now)
	if err != nil {
		return 0, err
	}
	if c.I == 0 {
		return 0, errChatToken
	}
	return c.I, nil
}

// chatImagePath 站内相对的查看链接（访客挂件、后台经同源代理访问）。
func chatImagePath(msgID uint64, ttl time.Duration) string {
	tok := signChatImageToken(msgID, ttl)
	if tok == "" {
		return ""
	}
	return "/api/chat/images/" + tok
}

// chatImageURL 绝对的查看链接（Slack 频道用），挂在该品牌官网域名下（官网把 /api/* 代理到 Center）。
func chatImageURL(b Brand, msgID uint64, ttl time.Duration) string {
	p := chatImagePath(msgID, ttl)
	if p == "" {
		return ""
	}
	return strings.TrimRight(b.Config().BaseURL, "/") + p
}

// chatImageAIURL 交给 AI 的 S3 临时地址；存储不可用时返回 ""（AI 只看到"[图片]"字样）。
func chatImageAIURL(ctx context.Context, key string) string {
	store := chatImages()
	if store == nil || key == "" {
		return ""
	}
	u, err := store.PresignGet(ctx, key, chatImageAITTL)
	if err != nil {
		return ""
	}
	return u
}
