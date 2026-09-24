-- Dữ liệu mẫu cho môi trường demo.
INSERT INTO users (id, email, display_name) VALUES
    (1, 'minhanh@example.com', 'Minh Anh'),
    (2, 'hoangnam@example.com', 'Hoàng Nam')
ON CONFLICT (id) DO NOTHING;

SELECT setval('users_id_seq', (SELECT MAX(id) FROM users));

INSERT INTO submissions (id, user_id, type, title, content, band_score, feedback) VALUES
(101, 1, 'writing_submission',
 'IELTS Writing Task 2 – Technology in Education',
 'Some people believe that technology has made learning easier, while others argue that it distracts students. In my opinion, the benefits clearly outweigh the drawbacks when technology is used with clear goals.

Firstly, online platforms give learners access to high-quality materials regardless of where they live. A student in a small town can now practise IELTS with the same resources as one in a big city.

Secondly, instant feedback helps students correct mistakes early. Automated tools highlight grammar errors, while teachers can focus on ideas and structure.

In conclusion, technology is a powerful tool for education, provided that learners use it with discipline and teachers guide them properly.',
 7.5,
 'Task Response tốt, lập luận rõ ràng. Nên đa dạng hoá cấu trúc câu phức và bổ sung ví dụ cụ thể hơn ở đoạn 2.'),
(102, 1, 'speaking_submission',
 'IELTS Speaking Part 2 – Describe a book you enjoyed',
 '[Transcript] The book I would like to talk about is "Atomic Habits" by James Clear...',
 7.0,
 'Fluency tốt, phát âm rõ. Cần mở rộng vốn từ vựng chủ đề.'),
(201, 2, 'writing_submission',
 'IELTS Writing Task 1 – Line graph: Coffee consumption',
 'The line graph illustrates coffee consumption in three countries between 2000 and 2020...',
 6.5,
 'Overview chưa nêu được xu hướng chính.')
ON CONFLICT (id) DO NOTHING;

SELECT setval('submissions_id_seq', (SELECT MAX(id) FROM submissions));
