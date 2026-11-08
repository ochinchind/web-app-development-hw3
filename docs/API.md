# HR Service API

Base URL: `http://localhost:8080` · Interactive docs (Swagger UI): `http://localhost:8081`

All request and response bodies are JSON (`Content-Type: application/json`). Field names are `snake_case`.
IDs are UUIDs. Dates are `YYYY-MM-DD`, clock times `HH:MM` (24h, office time zone `APP_TIMEZONE`, default
`Asia/Almaty`), months `YYYY-MM`, timestamps RFC 3339 in UTC. Money amounts are numbers with up to 2 decimals (KZT).
Enum values (`ACTIVE`, `VACATION`, ...) are accepted case-insensitively.

## Data model

```
                 ┌──────────────┐
                 │  Department  │
                 └──────┬───────┘
                        │ 1
                        │ N
┌──────────────┐ 1    N ┌──────────────┐ 1    N ┌───────────────────┐
│ WorkSchedule │────────│   Employee   │────────│ PerformanceReview │  (employee_id)
└──────────────┘        └──────┬───────┘────────│                   │  (reviewer_id)
                               │ 1        1   N └───────────────────┘
               ┌───────────────┼────────────────┐
               │ N             │ N              │ N
      ┌────────────────┐ ┌───────────┐   ┌───────────┐
      │AttendanceRecord│ │  Payslip  │   │   Leave   │
      └────────────────┘ └───────────┘   └───────────┘
```

| Relationship | Type | Column | On delete of the parent |
|---|---|---|---|
| Department → Employees | one-to-many | `employees.department_id` (required) | **blocked** while the department has employees (409) |
| WorkSchedule → Employees | one-to-many | `employees.schedule_id` (optional) | employees keep existing, `schedule_id` becomes empty |
| Employee → PerformanceReviews | one-to-many | `performance_reviews.employee_id` | reviews are deleted |
| Employee → PerformanceReviews (as reviewer) | one-to-many | `performance_reviews.reviewer_id` (optional) | reviews keep existing, `reviewer_id` becomes empty |
| Employee → AttendanceRecords | one-to-many | `attendance_records.employee_id` | records are deleted |
| Employee → Payslips | one-to-many | `payslips.employee_id` | payslips are deleted |
| Employee → Leaves | one-to-many | `leaves.employee_id` | leaves are deleted |

## Endpoints (45)

| # | Method | URL | Description | Success |
|---|--------|-----|-------------|---------|
| | **Departments** | | | |
| 1 | POST | `/v1/departments` | Create department | 201 |
| 2 | GET | `/v1/departments` | List / search departments | 200 |
| 3 | GET | `/v1/departments/{id}` | Get department (headcount, average salary) | 200 |
| 4 | PUT | `/v1/departments/{id}` | Replace department | 200 |
| 5 | PATCH | `/v1/departments/{id}` | Partially update department | 200 |
| 6 | DELETE | `/v1/departments/{id}` | Delete an empty department | 204 |
| 7 | GET | `/v1/departments/{department_id}/employees` | **Related:** employees of a department | 200 |
| 8 | GET | `/v1/departments/{department_id}/attendance` | **Related:** who is in the office on a day | 200 |
| 9 | POST | `/v1/departments/{department_id}/payroll` | **Related:** create the month's payslips for the department | 201 / 200 |
| 10 | GET | `/v1/departments/{department_id}/payroll` | **Related:** payroll totals for a month | 200 |
| | **Employees** | | | |
| 11 | POST | `/v1/employees` | Create employee | 201 |
| 12 | GET | `/v1/employees` | List / filter employees | 200 |
| 13 | GET | `/v1/employees/{id}` | Get employee | 200 |
| 14 | PUT | `/v1/employees/{id}` | Replace employee | 200 |
| 15 | PATCH | `/v1/employees/{id}` | Partially update employee (salary, status, ...) | 200 |
| 16 | DELETE | `/v1/employees/{id}` | Delete employee and their records | 204 |
| | **Work schedules** | | | |
| 17 | POST | `/v1/schedules` | Create schedule | 201 |
| 18 | GET | `/v1/schedules` | List schedules | 200 |
| 19 | GET | `/v1/schedules/{id}` | Get schedule | 200 |
| 20 | PUT | `/v1/schedules/{id}` | Replace schedule | 200 |
| 21 | DELETE | `/v1/schedules/{id}` | Delete schedule | 204 |
| 22 | GET | `/v1/schedules/{schedule_id}/employees` | **Related:** employees on a schedule | 200 |
| | **Performance reviews** | | | |
| 23 | POST | `/v1/employees/{employee_id}/performance-reviews` | Create review for an employee | 201 |
| 24 | GET | `/v1/employees/{employee_id}/performance-reviews` | **Related:** reviews of an employee + average | 200 |
| 25 | GET | `/v1/performance-reviews/{id}` | Get review | 200 |
| 26 | PATCH | `/v1/performance-reviews/{id}` | Partially update review | 200 |
| 27 | DELETE | `/v1/performance-reviews/{id}` | Delete review | 204 |
| | **Office attendance** | | | |
| 28 | POST | `/v1/employees/{employee_id}/attendance/check-in` | Check in now | 201 |
| 29 | POST | `/v1/employees/{employee_id}/attendance/check-out` | Check out now | 200 |
| 30 | POST | `/v1/employees/{employee_id}/attendance` | Add a record for a past day | 201 |
| 31 | GET | `/v1/employees/{employee_id}/attendance` | **Related:** attendance of an employee + totals | 200 |
| 32 | DELETE | `/v1/attendance/{id}` | Delete record | 204 |
| | **Payroll** | | | |
| 33 | POST | `/v1/employees/{employee_id}/payslips` | Create payslip | 201 |
| 34 | GET | `/v1/employees/{employee_id}/payslips` | **Related:** payslips of an employee | 200 |
| 35 | GET | `/v1/payslips/{id}` | Get payslip | 200 |
| 36 | PATCH | `/v1/payslips/{id}` | Change a DRAFT payslip | 200 |
| 37 | POST | `/v1/payslips/{id}/pay` | DRAFT → PAID | 200 |
| 38 | DELETE | `/v1/payslips/{id}` | Delete a DRAFT payslip | 204 |
| | **Leaves** | | | |
| 39 | POST | `/v1/employees/{employee_id}/leaves` | Request leave | 201 |
| 40 | GET | `/v1/employees/{employee_id}/leaves` | **Related:** leaves of an employee | 200 |
| 41 | GET | `/v1/leaves` | List / filter all leaves (approval queue) | 200 |
| 42 | GET | `/v1/leaves/{id}` | Get leave | 200 |
| 43 | POST | `/v1/leaves/{id}/approve` | PENDING → APPROVED | 200 |
| 44 | POST | `/v1/leaves/{id}/reject` | PENDING → REJECTED | 200 |
| 45 | DELETE | `/v1/leaves/{id}` | Withdraw a PENDING leave | 204 |

## Status codes and errors

| Status | When | `code` in body |
|--------|------|------|
| 200 OK | successful read / update / state change | – |
| 201 Created | a resource was created | – |
| 204 No Content | successful `DELETE` (empty body) | – |
| 400 Bad Request | validation failed: missing or too long field, bad format (UUID, email, date, time, month), value out of range, unknown reference in the body, empty PATCH, malformed JSON, wrong field type, invalid enum value, unknown query parameter, bad paging | 3 `INVALID_ARGUMENT` |
| 404 Not Found | the resource in the URL does not exist, or the route does not exist | 5 `NOT_FOUND` |
| 409 Conflict | duplicate of a unique value (email, department/schedule name, one review per quarter, one payslip per month, one attendance record per day) | 6 `ALREADY_EXISTS` |
| 409 Conflict | not allowed in the current state: paying/changing a PAID payslip, deciding an already decided leave, overlapping leave, checking in twice, deleting a department with employees, actions on a TERMINATED employee | 9 `FAILED_PRECONDITION` |
| 500 Internal Server Error | unexpected error | 13 `INTERNAL` |

Every error response has the same shape:

```json
{
  "code": 9,
  "message": "payslip was already paid at 2026-11-08T17:13:42Z",
  "details": []
}
```

## Paging (list endpoints)

| Param | Type | Default | Rules |
|-------|------|---------|-------|
| `limit` | int | 20 | 1..100, otherwise `400` |
| `offset` | int | 0 | ≥ 0, otherwise `400` |

List responses contain `total`, which is the number of matching rows before paging.

---

## Departments

### Department object

```json
{
  "id": "2caa26c3-5f11-4fb3-81ee-6043710b0658",
  "name": "Engineering",
  "description": "Product engineering",
  "location": "Almaty, Abay Ave 10, floor 6",
  "employee_count": 2,
  "average_salary": 1075000,
  "created_at": "2026-11-08T17:13:41.012345Z",
  "updated_at": "2026-11-08T17:13:41.012345Z"
}
```

`employee_count` and `average_salary` are computed over the department's employees whose status is not `TERMINATED`.

| Field | Rules |
|-------|-------|
| `name` | required, max 100 characters, unique (`409`) |
| `description` | optional, max 1000 |
| `location` | optional, max 200 |

### 1. `POST /v1/departments`

Body: `{ "name": "Engineering", "description": "Product development", "location": "Almaty, Abay Ave 10" }`

Response **201** with the Department object. Returns `400` or `409`.

### 2. `GET /v1/departments`

Query: `q` (name search), `limit`, `offset`. Response **200**: `{ "departments": [ …Department… ], "total": 2 }`

### 3. `GET /v1/departments/{id}`

Response **200** with the Department object. Returns `400` (invalid UUID) or `404`.

### 4. `PUT /v1/departments/{id}`

Full replace with the same body as create. Response **200**. Returns `400`, `404` or `409`.

### 5. `PATCH /v1/departments/{id}`

Only the fields that are sent change, for example `{ "description": "Product engineering" }`.
Response **200**. Returns `400` (empty body or invalid field), `404` or `409`.

### 6. `DELETE /v1/departments/{id}`

Response **204**. Returns `409` while the department still has employees (move them with
`PATCH /v1/employees/{id}` `{ "department_id": "…" }`, or delete them first) and `404` if it is unknown.

### 7. `GET /v1/departments/{department_id}/employees`

Query: `status` (`ACTIVE` / `ON_LEAVE` / `TERMINATED`), `limit`, `offset`.
Response **200**: `{ "employees": [ …Employee… ], "total": 2 }`. Returns `404` for an unknown department.

### 8. `GET /v1/departments/{department_id}/attendance`

Query: `date` (`YYYY-MM-DD`, defaults to today). The response lists every non-terminated employee hired by that date with
their presence status:

| Status | Meaning |
|--------|---------|
| `PRESENT` | has an attendance record, on time |
| `LATE` | has an attendance record, checked in after `start_time + late_grace_minutes` |
| `ABSENT` | working day, no record |
| `LEAVE` | an APPROVED leave covers the day, or the employee's status is `ON_LEAVE` |
| `DAY_OFF` | not a working day of the employee's schedule (no schedule means Monday–Friday) |

```json
{
  "department_id": "2caa26c3-…",
  "date": "2026-11-02",
  "present_count": 1,
  "late_count": 1,
  "absent_count": 1,
  "leave_count": 0,
  "day_off_count": 0,
  "entries": [
    { "employee": { "id": "…", "full_name": "Daniyar Akhmetov", "position": "Team lead" }, "status": "ABSENT", "check_in": "", "check_out": "", "late_minutes": 0 },
    { "employee": { "id": "…", "full_name": "Aigerim Sadykova", "position": "Backend developer" }, "status": "LATE", "check_in": "09:25", "check_out": "18:10", "late_minutes": 25 }
  ]
}
```

`present_count` includes late employees. Returns `400` or `404`.

### 9. `POST /v1/departments/{department_id}/payroll`

Body: `{ "period": "2026-10" }`. Creates a DRAFT payslip (base = current salary, no bonus or deductions) for every
non-terminated employee of the department who has a salary, was hired by the end of that month and has no payslip
for it yet. It returns the department payroll (see 10) with `created_count`. The status is **201** when payslips were
created and **200** when everyone already had one. Returns `400` (invalid or future month) or `404`.

### 10. `GET /v1/departments/{department_id}/payroll`

Query: `period` (`YYYY-MM`, defaults to the current month).

```json
{
  "department_id": "2caa26c3-…",
  "period": "2026-10",
  "created_count": 0,
  "payslip_count": 2,
  "draft_count": 1,
  "paid_count": 1,
  "total_base": 2150000,
  "total_bonus": 150000,
  "total_deductions": 95000,
  "total_net": 2205000,
  "payslips": [ …Payslip… ]
}
```

---

## Employees

### Employee object

```json
{
  "id": "857370a6-58b5-472a-83a9-f048b05bc24f",
  "first_name": "Aigerim",
  "last_name": "Sadykova",
  "full_name": "Aigerim Sadykova",
  "email": "aigerim@example.com",
  "phone": "+77015551234",
  "position": "Backend developer",
  "department_id": "2caa26c3-…",
  "department": { "id": "2caa26c3-…", "name": "Engineering" },
  "schedule_id": "9c916e82-…",
  "schedule": { "id": "9c916e82-…", "name": "Office 5/2" },
  "salary": 850000,
  "hire_date": "2024-03-01",
  "status": "ACTIVE",
  "created_at": "2026-11-08T17:13:41.064982Z",
  "updated_at": "2026-11-08T17:13:41.064982Z"
}
```

`schedule_id` is `""` and `schedule` is absent when no schedule is assigned.

| Field | Rules |
|-------|-------|
| `department_id` | required, must reference an existing department (`400`) |
| `first_name`, `last_name` | required, max 100 |
| `email` | required, valid address, lower-cased, unique (`409`) |
| `phone` | optional; spaces, `-`, `(`, `)` are removed; then 7–15 digits, optional leading `+` |
| `position` | optional, max 100 |
| `salary` | monthly base salary, 0..1 000 000 000 |
| `hire_date` | `YYYY-MM-DD`, defaults to today, at most one year in the future |
| `schedule_id` | optional, must reference an existing schedule (`400`) |
| `status` | `ACTIVE` (default), `ON_LEAVE`, `TERMINATED` |

A `TERMINATED` employee cannot check in, get new payslips, request leave or be reviewed (`409`).

### 11. `POST /v1/employees`

```json
{
  "department_id": "2caa26c3-…",
  "first_name": "Aigerim",
  "last_name": "Sadykova",
  "email": "aigerim@example.com",
  "phone": "+7 701 555-12-34",
  "position": "Backend developer",
  "salary": 850000,
  "hire_date": "2024-03-01",
  "schedule_id": "9c916e82-…"
}
```

Response **201** with the Employee object. Returns `400` or `409`.

### 12. `GET /v1/employees`

| Param | Description |
|-------|-------------|
| `q` | case-insensitive search in full name, email and position |
| `department_id` | only this department |
| `status` | `ACTIVE`, `ON_LEAVE` or `TERMINATED` |
| `limit` / `offset` | paging |

Response **200**: `{ "employees": [ …Employee… ], "total": 2 }`, ordered by last name.

### 13. `GET /v1/employees/{id}`

Response **200** with the Employee object. Returns `400` or `404`.

### 14. `PUT /v1/employees/{id}`

Full replace of every field above, with the same body as create plus `status`. Omitted optional fields are reset
(no schedule, `status` ACTIVE, `hire_date` today). Response **200**. Returns `400`, `404` or `409`.

### 15. `PATCH /v1/employees/{id}`

Only the fields that are sent change. Typical uses:

- salary raise: `{ "salary": 950000 }`
- transfer to another department: `{ "department_id": "…" }`
- assign or remove a schedule: `{ "schedule_id": "…" }` / `{ "schedule_id": "" }`
- termination: `{ "status": "TERMINATED" }`

Response **200**. Returns `400`, `404` or `409`.

### 16. `DELETE /v1/employees/{id}`

Response **204**. The employee's performance reviews, attendance records, payslips and leaves are deleted too. Reviews
they wrote for others are kept, with `reviewer_id` set to `""`. Returns `404` for an unknown employee.

---

## Work schedules

### WorkSchedule object

```json
{
  "id": "9c916e82-b201-45d9-8c22-848a0d1dcd28",
  "name": "Office 5/2",
  "work_days": [1, 2, 3, 4, 5],
  "start_time": "09:00",
  "end_time": "18:00",
  "late_grace_minutes": 10,
  "hours_per_week": 45,
  "employee_count": 1,
  "created_at": "…",
  "updated_at": "…"
}
```

| Field | Rules |
|-------|-------|
| `name` | required, max 100, unique (`409`) |
| `work_days` | at least one ISO weekday, 1 = Monday … 7 = Sunday; duplicates are removed and the list is sorted |
| `start_time`, `end_time` | `HH:MM`, `end_time` later than `start_time` |
| `late_grace_minutes` | 0..240 (default 0) |

### 17. `POST /v1/schedules`

Body: `{ "name": "Office 5/2", "work_days": [1,2,3,4,5], "start_time": "09:00", "end_time": "18:00", "late_grace_minutes": 10 }`
Response **201**. Returns `400` or `409`.

### 18. `GET /v1/schedules`

Response **200**: `{ "schedules": [ …WorkSchedule… ] }`, ordered by name.

### 19. `GET /v1/schedules/{id}`

Response **200**. Returns `400` or `404`.

### 20. `PUT /v1/schedules/{id}`

Full replace with the same body as create. Response **200**. Returns `400`, `404` or `409`. Lateness that was
already recorded is not recalculated.

### 21. `DELETE /v1/schedules/{id}`

Response **204**. Employees on the schedule keep existing without a schedule. Returns `404` if it is unknown.

### 22. `GET /v1/schedules/{schedule_id}/employees`

Query: `limit`, `offset`. Response **200**: `{ "employees": [ …Employee… ], "total": 1 }`. Returns `404` if the schedule is unknown.

---

## Performance reviews

### PerformanceReview object

```json
{
  "id": "…",
  "employee_id": "857370a6-…",
  "employee": { "id": "857370a6-…", "full_name": "Aigerim Sadykova", "position": "Backend developer" },
  "reviewer_id": "5d1e…",
  "reviewer": { "id": "5d1e…", "full_name": "Daniyar Akhmetov", "position": "Team lead" },
  "period": "2026-Q3",
  "score": 4,
  "comment": "Delivered the payroll module on time.",
  "goals": "Mentor a junior developer",
  "review_date": "2026-10-01",
  "created_at": "…",
  "updated_at": "…"
}
```

| Field | Rules |
|-------|-------|
| `period` | required, `YYYY-Qn` (e.g. `2026-Q3`, case-insensitive); one review per employee per period (`409`) |
| `score` | required, 1..5 |
| `reviewer_id` | optional; must be an existing employee (`400`), not the reviewed employee (`400`) |
| `comment`, `goals` | optional, max 2000 each |
| `review_date` | `YYYY-MM-DD`, defaults to today, must not be in the future |

### 23. `POST /v1/employees/{employee_id}/performance-reviews`

Body: `{ "reviewer_id": "…", "period": "2026-Q3", "score": 4, "comment": "…", "goals": "…" }`
Response **201**. Returns `400`, `404` (unknown employee) or `409` (duplicate period, or the employee is TERMINATED).

### 24. `GET /v1/employees/{employee_id}/performance-reviews`

Query: `limit`, `offset`. Response **200**, with the newest period first:

```json
{ "reviews": [ …PerformanceReview… ], "total": 2, "average_score": 4.5 }
```

### 25. `GET /v1/performance-reviews/{id}`

Response **200**. Returns `400` or `404`.

### 26. `PATCH /v1/performance-reviews/{id}`

Any of `reviewer_id` (`""` removes it), `period`, `score`, `comment`, `goals`, `review_date`.
Response **200**. Returns `400`, `404` or `409`.

### 27. `DELETE /v1/performance-reviews/{id}`

Response **204**. Returns `404` if the review is unknown.

---

## Office attendance

### AttendanceRecord object

```json
{
  "id": "…",
  "employee_id": "857370a6-…",
  "date": "2026-11-02",
  "check_in": "09:25",
  "check_out": "18:10",
  "worked_minutes": 525,
  "late_minutes": 25,
  "is_late": true,
  "note": "Traffic jam",
  "created_at": "…",
  "updated_at": "…"
}
```

There is one record per employee per day. `check_out` is `""` while the employee is still in the office.
Lateness is calculated when the record is created, from the employee's schedule. If the check-in is later than
`start_time + late_grace_minutes` on a working day, `late_minutes` is the number of minutes after `start_time`.
Employees without a schedule are never late. Attendance is refused (`409`) for TERMINATED employees and on days
covered by an APPROVED leave.

### 28. `POST /v1/employees/{employee_id}/attendance/check-in`

Body `{ "note": "optional" }` (or `{}`). Creates today's record with the current time.
Response **201**. Returns `404` or `409` (already checked in today, terminated, on leave).

### 29. `POST /v1/employees/{employee_id}/attendance/check-out`

Body `{}`. Sets `check_out` on today's record to the current time.
Response **200**. Returns `404` or `409` (not checked in today, or already checked out).

### 30. `POST /v1/employees/{employee_id}/attendance`

Adds a record for any day from the hire date up to today (HR correction or import).

```json
{ "date": "2026-11-02", "check_in": "09:25", "check_out": "18:10", "note": "Traffic jam" }
```

`check_out` is optional and must be later than `check_in`. Response **201**. Returns `400`, `404` or `409`
(a record already exists for that day).

### 31. `GET /v1/employees/{employee_id}/attendance`

Query: `date_from`, `date_to` (inclusive, optional), `limit`, `offset`. Records are returned newest first, and the
summary covers the whole date range:

```json
{
  "records": [ …AttendanceRecord… ],
  "total": 2,
  "summary": { "days_present": 2, "days_late": 1, "total_worked_minutes": 1055, "total_late_minutes": 25 }
}
```

### 32. `DELETE /v1/attendance/{id}`

Response **204**. Returns `404` if the record is unknown.

---

## Payroll

### Payslip object

```json
{
  "id": "…",
  "employee_id": "857370a6-…",
  "employee": { "id": "857370a6-…", "full_name": "Aigerim Sadykova", "position": "Backend developer" },
  "period": "2026-10",
  "base_amount": 950000,
  "bonus": 150000,
  "deductions": 95000,
  "net_amount": 1005000,
  "status": "PAID",
  "paid_at": "2026-11-08T17:13:42.353794Z",
  "created_at": "…",
  "updated_at": "…"
}
```

`net_amount = base_amount + bonus − deductions` and must not be negative (`400`). There is one payslip per employee per
month (`409`). The lifecycle is `DRAFT` → `PAID`, and a PAID payslip can no longer be changed or deleted (`409`).
`paid_at` is present only once PAID.

### 33. `POST /v1/employees/{employee_id}/payslips`

```json
{ "period": "2026-10", "bonus": 100000, "deductions": 95000 }
```

`period` is required (`YYYY-MM`, not in the future, not before the hire month). `base_amount` is optional and defaults
to the employee's current salary. `bonus` and `deductions` default to 0. Response **201** as DRAFT. Returns `400`, `404`
or `409` (duplicate month, or the employee is TERMINATED).

### 34. `GET /v1/employees/{employee_id}/payslips`

Query: `status` (`DRAFT` / `PAID`), `limit`, `offset`. Response **200**: `{ "payslips": [ … ], "total": 1 }`, newest month first.

### 35. `GET /v1/payslips/{id}`

Response **200**. Returns `400` or `404`.

### 36. `PATCH /v1/payslips/{id}`

Any of `base_amount`, `bonus`, `deductions`; `net_amount` is recalculated. Only for DRAFT payslips.
Response **200**. Returns `400`, `404` or `409` (already PAID).

### 37. `POST /v1/payslips/{id}/pay`

Body `{}`. Moves DRAFT → PAID and sets `paid_at`. Response **200**. Returns `404` or `409` (already paid).

### 38. `DELETE /v1/payslips/{id}`

Response **204**. Only DRAFT payslips can be deleted (`409` otherwise). Returns `404` if the payslip is unknown.

---

## Leaves

### Leave object

```json
{
  "id": "…",
  "employee_id": "857370a6-…",
  "employee": { "id": "857370a6-…", "full_name": "Aigerim Sadykova", "position": "Backend developer" },
  "type": "VACATION",
  "start_date": "2026-12-08",
  "end_date": "2026-12-21",
  "days": 14,
  "reason": "Family trip",
  "status": "APPROVED",
  "decision_comment": "Enjoy your trip!",
  "decided_at": "…",
  "created_at": "…",
  "updated_at": "…"
}
```

| Field | Rules |
|-------|-------|
| `type` | required: `VACATION`, `SICK_LEAVE`, `UNPAID_LEAVE` |
| `start_date`, `end_date` | required, `YYYY-MM-DD`, inclusive, `end_date ≥ start_date`, at most 366 days, not before the hire date |
| `reason` | optional, max 1000 |

The lifecycle is `PENDING` → `APPROVED` or `REJECTED`. A new leave must not overlap a PENDING or APPROVED leave of
the same employee (`409`). `days` counts calendar days. An approved leave shows up as `LEAVE` in the department
attendance view and blocks check-ins on those days.

### 39. `POST /v1/employees/{employee_id}/leaves`

Body: `{ "type": "VACATION", "start_date": "2026-12-08", "end_date": "2026-12-21", "reason": "Family trip" }`
Response **201** as PENDING. Returns `400`, `404` or `409` (overlap, or the employee is TERMINATED).

### 40. `GET /v1/employees/{employee_id}/leaves`

Query: `status`, `limit`, `offset`. Response **200**: `{ "leaves": [ … ], "total": 2 }`, latest start date first.

### 41. `GET /v1/leaves`

Query: `status`, `type`, `department_id`, `limit`, `offset`. For example, `GET /v1/leaves?status=PENDING&department_id=…`
returns a department's approval queue. Response **200**: `{ "leaves": [ … ], "total": 3 }`.

### 42. `GET /v1/leaves/{id}`

Response **200**. Returns `400` or `404`.

### 43. `POST /v1/leaves/{id}/approve`

Body: `{ "comment": "Enjoy your trip!" }` (optional). Sets `status`, `decision_comment` and `decided_at`.
Response **200**. Returns `404` or `409` (not PENDING). The transition is atomic, so a leave cannot be decided twice.

### 44. `POST /v1/leaves/{id}/reject`

Same as approve, with the result `REJECTED`.

### 45. `DELETE /v1/leaves/{id}`

Withdraws a request. Response **204**. Only PENDING leaves can be deleted (`409` otherwise). Returns `404` if the leave is unknown.
