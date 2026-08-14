---
type: table
title: products
description: Product catalog master data.
tags: [core, catalog]
---

## Schema

| Column      | Type   | Description             |
|-------------|--------|-------------------------|
| product_id  | INT64  | Primary key             |
| name        | STRING | Product name            |
| category    | STRING | Product category        |
| price       | NUMERIC| Unit price              |

## Related

- [orders](../tables/orders.md)
