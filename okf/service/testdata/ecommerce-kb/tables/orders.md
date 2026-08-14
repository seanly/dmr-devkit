---
type: table
title: orders
description: Core order transaction table.
tags: [core, transactions]
---

## Schema

| Column       | Type      | Description                |
|--------------|-----------|----------------------------|
| order_id     | INT64     | Primary key                |
| customer_id  | INT64     | Foreign key to customers   |
| product_id   | INT64     | Foreign key to products    |
| order_date   | TIMESTAMP | When the order was placed  |
| amount       | NUMERIC   | Order total amount         |

## Related

- [customers](customers.md)
- [products](../products/products.md)
