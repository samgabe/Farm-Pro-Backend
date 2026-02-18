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
  price_per_unit NUMERIC(12,2) NOT NULL,
  total_amount NUMERIC(12,2) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS reports (
  id SERIAL PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  category TEXT NOT NULL,
  last_generated DATE NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_animals_status ON animals(status, health_status);
CREATE INDEX IF NOT EXISTS idx_health_next_due ON health_records(next_due);
CREATE INDEX IF NOT EXISTS idx_production_log_date ON production_logs(log_date);
CREATE INDEX IF NOT EXISTS idx_expense_date ON expenses(expense_date);
CREATE INDEX IF NOT EXISTS idx_sale_date ON sales(sale_date);
