---
type: table
title: customers
description: Customer master data.
tags: [core, master-data]
---

## Schema

| Column      | Type   | Description          |
|-------------|--------|----------------------|
| customer_id | INT64  | Primary key          |
| name        | STRING | Customer full name   |
| email       | STRING | Contact email        |
| region      | STRING | Geographic region    |

## Related

- [orders](orders.md)
