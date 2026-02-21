INSERT INTO users(name, email, password_hash, role_id, role, phone, status, email_verified)
SELECT
  'John Farmer',
  'john@farmpro.com',
  '$2a$10$Rr4DmKcj4ox5Y/JReFH9NuzGW83CgT8NQ.Ww7EzDcU0D00/FFBgNW',
  r.id,
  r.name,
  '',
  'active',
  true
FROM roles r
WHERE r.name = 'owner'
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.email = 'john@farmpro.com');
