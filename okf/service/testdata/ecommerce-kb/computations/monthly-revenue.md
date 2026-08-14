---
type: Attested Computation
title: Monthly Revenue Computation
description: Computes total revenue per month.
runtime: bigquery
executor:
  resource: references/revenue_engine.sql
  receipt: [job_id, query_hash]
attester:
  resource: references/finance_team.py
---

## Logic

```sql
SELECT
  DATE_TRUNC(order_date, MONTH) AS month,
  SUM(amount) AS revenue
FROM orders
GROUP BY 1
ORDER BY 1
```

## Related

- [orders](../tables/orders.md)
