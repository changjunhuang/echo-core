package service

import (
	"context"
	"errors"
	"github.com/qiniu/go-sdk/v7/auth"
	"github.com/qiniu/go-sdk/v7/storage"
	"io"
	"log"
	"time"
)

var (
	accessKey = "L1PcZ8nUNX8XdeGSH0VcdlB5GsjsLfpf3qZ5-4iU"
	secretKey = "U_5FvdBhV-LCKdvBy0T8tSkfErZamQNiHSZA_eHv"
	bucket    = "huangchangjun"
	domain    = "tc155y2lr.hn-bkt.clouddn.com"
)

type FileService struct {
}

func NewFileService() *FileService {
	return &FileService{}
}

// checkConfig 检查七牛云配置是否完整
func checkConfig() error {
	if accessKey == "" || secretKey == "" || bucket == "" || domain == "" {
		return errors.New("七牛云配置不完整，请检查 accessKey、secretKey、bucket 和 domain")
	}
	log.Println("七牛云配置检查通过")
	return nil
}

func (h *FileService) UploadToQiniu(file io.Reader, key string) (string, error) {
	// 检查配置
	if err := checkConfig(); err != nil {
		log.Println(err)
		return "", err
	}
	// 创建凭证
	putPolicy := storage.PutPolicy{
		Scope: bucket + ":" + key,
	}
	mac := auth.New(accessKey, secretKey)
	upToken := putPolicy.UploadToken(mac)

	// 配置存储区域
	cfg := storage.Config{}
	// 根据bucket所在区域设置，这里假设华东
	cfg.Zone = &storage.ZoneHuanan
	// 是否使用https
	cfg.UseHTTPS = false
	// 构建表单上传的对象
	formUploader := storage.NewFormUploader(&cfg)
	ret := storage.PutRet{}
	// 可选配置
	putExtra := storage.PutExtra{}

	err := formUploader.Put(context.Background(), &ret, upToken, key, file, -1, &putExtra)
	if err != nil {
		log.Println("上传七牛云失败：", ret, err)
		return "", err
	}
	log.Println("上传七牛云成功：", ret)
	// 返回完整的访问URL
	return domain + "/" + ret.Key, nil
}

// getPrivateURL 生成七牛云私有空间临时访问链接
// 如果是公开空间，直接返回拼接的 URL（无需签名）
func (h *FileService) GetPrivateURL(key string, expiresInSeconds int64) (string, error) {
	// 检查配置
	if err := checkConfig(); err != nil {
		log.Println(err)
		return "", err
	}
	// 如果空间是公开的，直接返回拼接 URL（根据你的实际情况判断）
	// 此处假设为私有空间，需要签名。若是公开空间，可注释下面签名代码，直接返回 qiniuDomain + "/" + key

	mac := auth.New(accessKey, secretKey)
	// 构建私有空间访问 URL
	// 注意：如果使用了 CDN 域名且 CDN 开启了防盗链，可能需要额外处理
	// 这里使用最简单的私有空间签名方法
	//urlToSign := fmt.Sprintf("%s,%s", domain, key)

	deadline := time.Now().Add(time.Duration(expiresInSeconds) * time.Second).Unix()
	privateURL := storage.MakePrivateURL(mac, domain, key, deadline)
	log.Println("七牛云文件url获取成功：", privateURL)
	return privateURL, nil
}

// getPublicURL 生成七牛云公开空间访问链接
func (h *FileService) GetPublicURL(key string) (string, error) {
	// 检查配置
	if err := checkConfig(); err != nil {
		log.Println(err)
		return "", err
	}

	publicURL := storage.MakePublicURL(domain, key)
	log.Println("七牛云文件url获取成功：", publicURL)
	return publicURL + ".png", nil
}
