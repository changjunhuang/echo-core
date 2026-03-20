package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go-start/service"
	"net/http"
	"path"
)

type FileHandler struct {
	service *service.FileService
}

func NewFileHandler() (*FileHandler, error) {
	fileService, err := service.NewFileService()
	if err != nil {
		return nil, err
	}
	return &FileHandler{service: fileService}, nil
}

// uploadHandler 处理文件上传
func (h *FileHandler) UploadHandler(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file service not initialized"})
		return
	}

	// 1. 限制请求体大小（例如 50MB）
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 50<<20)

	// 2. 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法获取文件，请使用字段名 'file'"})
		return
	}
	defer func() {
		_ = file.Close()
	}()

	// 3. 可选：验证文件类型（例如只允许图片和视频）
	// 可以通过检测 Content-Type 或扩展名来实现
	// 这里只做简单示例，实际可根据需求放开
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".mp4": true, ".mov": true, ".avi": true, ".mkv": true,
		".pdf": true, ".doc": true, ".docx": true,
	}
	ext := path.Ext(header.Filename)
	if !allowedExts[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件类型"})
		return
	}

	// 4. 生成唯一文件名（保留原始扩展名）
	newFilename := uuid.New().String() + ext

	// 5. 上传到七牛云
	url, err := h.service.Upload(file, newFilename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "上传到七牛云失败: " + err.Error()})
		return
	}

	// 6. 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"url":  url,
		"key":  newFilename,
		"size": header.Size,
	})
}

// downloadRedirectHandler 处理文件下载重定向
func (h *FileHandler) DownloadRedirectHandler(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file service not initialized"})
		return
	}

	// Query 参数中获取文件 key
	key := c.Query("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件key参数"})
		return
	}

	// 生成七牛云公有空间临时下载 URL
	downloadURL, err := h.service.GetPublicURL(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成下载链接失败"})
		return
	}

	// 重定向到七牛云临时 URL
	//c.Redirect(http.StatusFound, downloadURL)

	c.JSON(http.StatusOK, gin.H{
		"downloadURL": downloadURL,
	})
}
