package domain

// Snapshot là nội dung bài làm được hiển thị trên trang chia sẻ (read model).
type Snapshot struct {
	OwnerName string
	Title     string
	Content   string
	BandScore float32
	Feedback  string
}
