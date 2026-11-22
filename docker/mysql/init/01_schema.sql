-- Sample shop schema + data for development. Runs once on first container start.
USE shop;

CREATE TABLE customers (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(150) NOT NULL UNIQUE,
    city VARCHAR(80),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) COMMENT = 'People who registered in the shop';

CREATE TABLE products (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    category VARCHAR(60) NOT NULL,
    price DECIMAL(10,2) NOT NULL
);

CREATE TABLE orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id INT NOT NULL,
    status ENUM('pending','paid','refunded','cancelled') NOT NULL DEFAULT 'pending',
    total DECIMAL(12,2) NOT NULL,
    ordered_at DATETIME NOT NULL,
    CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id) REFERENCES customers(id),
    INDEX idx_orders_ordered_at (ordered_at)
);

-- Intentionally NO foreign keys here, to test relationship inference later.
CREATE TABLE order_items (
    id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    product_id INT NOT NULL,
    quantity INT NOT NULL,
    unit_price DECIMAL(10,2) NOT NULL
);

INSERT INTO customers (name, email, city, created_at) VALUES
('Ali Khan',      'ali@example.com',    'Lahore',    '2025-01-10 10:00:00'),
('Sara Ahmed',    'sara@example.com',   'Karachi',   '2025-02-14 12:30:00'),
('Bilal Shah',    'bilal@example.com',  'Islamabad', '2025-03-05 09:15:00'),
('Hina Malik',    'hina@example.com',   'Lahore',    '2025-04-20 16:45:00'),
('Usman Tariq',   'usman@example.com',  'Karachi',   '2025-06-01 11:00:00'),
('Ayesha Noor',   'ayesha@example.com', 'Multan',    '2025-08-12 14:20:00'),
('Hamza Iqbal',   'hamza@example.com',  'Lahore',    '2026-01-03 08:00:00'); -- never ordered

INSERT INTO products (name, category, price) VALUES
('Laptop',     'Electronics', 150000.00),
('Headphones', 'Electronics',   8000.00),
('Desk Chair', 'Furniture',    25000.00),
('Notebook',   'Stationery',     300.00),
('Monitor',    'Electronics',  45000.00);

INSERT INTO orders (customer_id, status, total, ordered_at) VALUES
(1, 'paid',     158000.00, '2026-01-15 10:00:00'),
(1, 'paid',      45000.00, '2026-10-02 13:00:00'),
(2, 'paid',      25300.00, '2026-03-22 17:30:00'),
(2, 'refunded',   8000.00, '2026-04-01 09:00:00'),
(3, 'paid',     150000.00, '2026-11-05 15:00:00'),
(4, 'pending',   45000.00, '2026-11-18 11:45:00'),
(5, 'paid',        900.00, '2025-12-10 10:10:00'),
(6, 'cancelled', 25000.00, '2026-06-30 19:00:00');

INSERT INTO order_items (order_id, product_id, quantity, unit_price) VALUES
(1, 1, 1, 150000.00), (1, 2, 1, 8000.00),
(2, 5, 1, 45000.00),
(3, 3, 1, 25000.00), (3, 4, 1, 300.00),
(4, 2, 1, 8000.00),
(5, 1, 1, 150000.00),
(6, 5, 1, 45000.00),
(7, 4, 3, 300.00),
(8, 3, 1, 25000.00);

-- Read-only account the agent uses (SELECT only = database-level safety).
CREATE USER 'agent_ro'@'%' IDENTIFIED BY 'agent_ro_pass';
GRANT SELECT ON shop.* TO 'agent_ro'@'%';
FLUSH PRIVILEGES;
