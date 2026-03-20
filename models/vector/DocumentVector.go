package vector

// DocumentVector 表示要存储的文档向量数据
type DocumentVector struct {
	FileID   string
	Filename string
	Vector   []float32
	Metadata map[string]interface{} // 额外的属性
}
