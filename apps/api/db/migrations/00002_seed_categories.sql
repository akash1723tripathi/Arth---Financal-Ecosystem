-- The 21 system categories (user_id NULL, stable key). Keys and kinds match apps/mobile/constants/categories.ts.
-- Display names, icons and colours are the values used by the Welth boilerplate (third-party; see docs/MIGRATION.md §7).
-- Idempotent: re-running never duplicates or overwrites a system category.
-- sort_order: expenses 10..150, income 210..260 (gaps leave room for later inserts); "other" is last in each kind.

-- +goose Up
INSERT INTO categories (user_id, key, name, kind, icon, color, sort_order) VALUES
  (NULL, 'food',          'Food & Dining', 'EXPENSE', '🍔', '#FF6B6B',  10),
  (NULL, 'groceries',     'Groceries',     'EXPENSE', '🛒', '#FF9F43',  20),
  (NULL, 'transport',     'Transport',     'EXPENSE', '🚗', '#4ECDC4',  30),
  (NULL, 'shopping',      'Shopping',      'EXPENSE', '🛍️', '#45B7D1',  40),
  (NULL, 'entertainment', 'Entertainment', 'EXPENSE', '🎬', '#96CEB4',  50),
  (NULL, 'health',        'Health',        'EXPENSE', '💊', '#FF8B94',  60),
  (NULL, 'utilities',     'Utilities',     'EXPENSE', '💡', '#FFEAA7',  70),
  (NULL, 'rent',          'Rent',          'EXPENSE', '🏠', '#DDA0DD',  80),
  (NULL, 'education',     'Education',     'EXPENSE', '📚', '#98D8C8',  90),
  (NULL, 'travel',        'Travel',        'EXPENSE', '✈️', '#F7DC6F', 100),
  (NULL, 'insurance',     'Insurance',     'EXPENSE', '🛡️', '#A29BFE', 110),
  (NULL, 'subscriptions', 'Subscriptions', 'EXPENSE', '📱', '#FD79A8', 120),
  (NULL, 'emi',           'EMI / Loan',    'EXPENSE', '🏦', '#E17055', 130),
  (NULL, 'personal_care', 'Personal Care', 'EXPENSE', '💇', '#FD9644', 140),
  (NULL, 'other',         'Other',         'EXPENSE', '📦', '#BDC3C7', 150),
  (NULL, 'salary',        'Salary',        'INCOME',  '💼', '#2ECC71', 210),
  (NULL, 'freelance',     'Freelance',     'INCOME',  '💻', '#27AE60', 220),
  (NULL, 'business',      'Business',      'INCOME',  '🏢', '#00B894', 230),
  (NULL, 'investment',    'Investment',    'INCOME',  '📈', '#1ABC9C', 240),
  (NULL, 'gift',          'Gift',          'INCOME',  '🎁', '#3498DB', 250),
  (NULL, 'other_income',  'Other Income',  'INCOME',  '💰', '#8E44AD', 260)
ON CONFLICT (key) WHERE user_id IS NULL DO NOTHING;

-- +goose Down
-- Removes only system rows. Fails (ON DELETE RESTRICT) if transactions still reference them, which is the safe outcome.
DELETE FROM categories WHERE user_id IS NULL AND key IN (
  'food','groceries','transport','shopping','entertainment','health','utilities','rent','education','travel',
  'insurance','subscriptions','emi','personal_care','other',
  'salary','freelance','business','investment','gift','other_income'
);
