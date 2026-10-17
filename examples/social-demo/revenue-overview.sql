-- MALLARD  /  COMMERCE ANALYTICS
-- Synthetic demo · SQL functions → stream → procedure → task → dynamic table

WITH market_performance AS (
    SELECT
        region,
        customers,
        orders,
        revenue,
        ROUND(revenue / NULLIF(orders, 0), 2) AS average_order
    FROM MALLARD_DEMO_DB.COMMERCE.REGIONAL_REVENUE
),
portfolio AS (
    SELECT SUM(revenue) AS total_revenue
    FROM market_performance
)
SELECT
    m.region                                    AS MARKET,
    m.customers                                 AS CUSTOMERS,
    m.orders                                    AS ORDERS,
    CAST(m.revenue AS DECIMAL(12, 2))            AS NET_REVENUE,
    CAST(m.average_order AS DECIMAL(12, 2))      AS AVG_ORDER,
    ROUND(100.0 * m.revenue / p.total_revenue, 1) AS REVENUE_SHARE_PCT,
    REVENUE_BAND(m.revenue) AS SIGNAL
FROM market_performance m
CROSS JOIN portfolio p
ORDER BY m.revenue DESC;
