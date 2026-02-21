CREATE TABLE IF NOT EXISTS users (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('owner', 'manager', 'worker', 'veterinarian')),
  phone TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS roles (
  id SERIAL PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,
  is_system BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
  id SERIAL PRIMARY KEY,
  key TEXT UNIQUE NOT NULL,
  description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS role_permissions (
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission_id INTEGER NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
  PRIMARY KEY (role_id, permission_id)
);

INSERT INTO roles(name, is_system) VALUES
  ('owner', TRUE),
  ('manager', TRUE),
  ('veterinarian', TRUE),
  ('worker', TRUE)
ON CONFLICT (name) DO NOTHING;

INSERT INTO permissions(key, description) VALUES
  ('dashboard.read', 'Access dashboard overview'),
  ('animals.read', 'Read animal records'),
  ('animals.write', 'Create, update, delete animals'),
  ('health.read', 'Read health records and schedules'),
  ('health.write', 'Create, update, delete health records'),
  ('breeding.read', 'Read breeding records'),
  ('breeding.write', 'Create, update, delete breeding records'),
  ('production.read', 'Read production data'),
  ('production.create', 'Create production logs'),
  ('production.manage', 'Update or delete production logs'),
  ('expenses.read', 'Read expenses data'),
  ('expenses.write', 'Create, update, delete expenses'),
  ('sales.read', 'Read sales data'),
  ('sales.write', 'Create, update, delete sales'),
  ('reports.read', 'Read reports and downloads'),
  ('reports.generate', 'Generate reports'),
  ('etims.manage', 'Generate and download local tax receipts'),
  ('users.read', 'Read users and user stats'),
  ('users.manage', 'Create, update, delete users')
ON CONFLICT (key) DO NOTHING;

UPDATE permissions
SET description = 'Generate and download local tax receipts'
WHERE key = 'etims.manage';

INSERT INTO role_permissions(role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
  'dashboard.read','animals.read','animals.write','health.read','health.write',
  'breeding.read','breeding.write','production.read','production.create','production.manage',
  'expenses.read','expenses.write','sales.read','sales.write','reports.read','reports.generate','etims.manage',
  'users.read','users.manage'
)
WHERE r.name = 'owner'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
  'dashboard.read','animals.read','animals.write','health.read','health.write',
  'breeding.read','breeding.write','production.read','production.create','production.manage',
  'expenses.read','expenses.write','sales.read','sales.write','reports.read','reports.generate','etims.manage',
  'users.read'
)
WHERE r.name = 'manager'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
  'dashboard.read','animals.read','health.read','health.write','reports.read'
)
WHERE r.name = 'veterinarian'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
  'dashboard.read','animals.read','production.read','production.create'
)
WHERE r.name = 'worker'
ON CONFLICT DO NOTHING;

ALTER TABLE users ADD COLUMN IF NOT EXISTS role_id INTEGER;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verify_token_hash TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verify_expires_at TIMESTAMP;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reset_token_hash TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reset_token_expires_at TIMESTAMP;

UPDATE users u
SET role_id = r.id
FROM roles r
WHERE u.role_id IS NULL AND LOWER(TRIM(u.role)) = r.name;

UPDATE users
SET role_id = (SELECT id FROM roles WHERE name = 'worker')
WHERE role_id IS NULL;

UPDATE users
SET email_verified = true
WHERE email_verified IS NULL;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_id_fkey;
ALTER TABLE users ADD CONSTRAINT users_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id);
ALTER TABLE users ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE users ALTER COLUMN email_verified SET NOT NULL;
ALTER TABLE users ALTER COLUMN email_verified SET DEFAULT false;

CREATE TABLE IF NOT EXISTS animals (
  id SERIAL PRIMARY KEY,
  tag_id TEXT UNIQUE NOT NULL,
  type TEXT NOT NULL,
  breed TEXT NOT NULL,
  birth_date DATE,
  weight_kg NUMERIC(10,2),
  health_status TEXT NOT NULL DEFAULT 'healthy' CHECK (health_status IN ('healthy', 'attention', 'sick')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'sold')),
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS health_records (
  id SERIAL PRIMARY KEY,
  animal_id INTEGER NOT NULL REFERENCES animals(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  treatment TEXT NOT NULL,
  record_date DATE NOT NULL,
  veterinarian TEXT NOT NULL,
  next_due DATE,
  notes TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS breeding_records (
  id SERIAL PRIMARY KEY,
  mother_animal_id INTEGER NOT NULL REFERENCES animals(id) ON DELETE CASCADE,
  father_animal_id INTEGER NOT NULL REFERENCES animals(id) ON DELETE CASCADE,
  species TEXT NOT NULL,
  breeding_date DATE NOT NULL,
  expected_birth_date DATE,
  actual_birth_date DATE,
  offspring_count INTEGER,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'cancelled')),
  notes TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS production_logs (
  id SERIAL PRIMARY KEY,
  log_date DATE NOT NULL UNIQUE,
  milk_liters NUMERIC(10,2) NOT NULL DEFAULT 0,
  eggs_count INTEGER NOT NULL DEFAULT 0,
  wool_kg NUMERIC(10,2) NOT NULL DEFAULT 0,
  total_value NUMERIC(12,2) NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS expenses (
  id SERIAL PRIMARY KEY,
  expense_date DATE NOT NULL,
  category TEXT NOT NULL,
  item TEXT NOT NULL,
  vendor TEXT NOT NULL,
  amount NUMERIC(12,2) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sales (
  id SERIAL PRIMARY KEY,
  sale_date DATE NOT NULL,
  product TEXT NOT NULL,
  quantity_value NUMERIC(12,2) NOT NULL,
  quantity_unit TEXT NOT NULL,
  buyer TEXT NOT NULL,
  buyer_pin TEXT NOT NULL DEFAULT '',
  delivery_county TEXT NOT NULL DEFAULT '',
  delivery_subcounty TEXT NOT NULL DEFAULT '',
  vat_applicable BOOLEAN NOT NULL DEFAULT FALSE,
  vat_rate NUMERIC(5,4) NOT NULL DEFAULT 0,
  vat_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  net_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  price_per_unit NUMERIC(12,2) NOT NULL,
  total_amount NUMERIC(12,2) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE sales ADD COLUMN IF NOT EXISTS buyer_pin TEXT NOT NULL DEFAULT '';
ALTER TABLE sales ADD COLUMN IF NOT EXISTS delivery_county TEXT NOT NULL DEFAULT '';
ALTER TABLE sales ADD COLUMN IF NOT EXISTS delivery_subcounty TEXT NOT NULL DEFAULT '';
ALTER TABLE sales ADD COLUMN IF NOT EXISTS vat_applicable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS vat_rate NUMERIC(5,4) NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS vat_amount NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS net_amount NUMERIC(12,2) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS reports (
  id SERIAL PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  category TEXT NOT NULL,
  last_generated DATE NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS etims_submissions (
  id SERIAL PRIMARY KEY,
  sale_id INTEGER NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
  invoice_number TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'local_generated' CHECK (status IN ('local_generated', 'submitted_local', 'accepted_local', 'rejected_local')),
  payload JSONB NOT NULL,
  submitted_at TIMESTAMP NOT NULL DEFAULT NOW(),
  response JSONB
);

ALTER TABLE etims_submissions DROP CONSTRAINT IF EXISTS etims_submissions_status_check;
ALTER TABLE etims_submissions ADD CONSTRAINT etims_submissions_status_check CHECK (status IN ('local_generated', 'submitted_local', 'accepted_local', 'rejected_local'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_etims_sale_id_unique ON etims_submissions(sale_id);

CREATE INDEX IF NOT EXISTS idx_animals_status ON animals(status, health_status);
CREATE INDEX IF NOT EXISTS idx_health_next_due ON health_records(next_due);
CREATE INDEX IF NOT EXISTS idx_production_log_date ON production_logs(log_date);
CREATE INDEX IF NOT EXISTS idx_expense_date ON expenses(expense_date);
CREATE INDEX IF NOT EXISTS idx_sale_date ON sales(sale_date);
CREATE INDEX IF NOT EXISTS idx_users_role_id ON users(role_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id ON role_permissions(role_id);
CREATE INDEX IF NOT EXISTS idx_users_email_verify_token_hash ON users(email_verify_token_hash);
CREATE INDEX IF NOT EXISTS idx_users_reset_token_hash ON users(reset_token_hash);
CREATE INDEX IF NOT EXISTS idx_etims_submitted_at ON etims_submissions(submitted_at DESC);
