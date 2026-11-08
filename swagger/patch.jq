# Adds API metadata and documents the real success codes that Envoy returns
# (201 for creating calls, 204 for Delete<Entity>) instead of the generator's default 200.
def created: test("_(Create|CheckIn|RunDepartmentPayroll)");
def deleted: test("_Delete(Department|Employee|Schedule|Review|Attendance|Payslip|Leave)$");

. + {
  info: {
    title: "HR Service API",
    version: "1.0",
    description: "Departments, employees, performance reviews, work schedules, office attendance, payroll and leaves. REST via Envoy gRPC-JSON transcoding."
  },
  host: "localhost:8080",
  schemes: ["http"]
}
| .paths |= map_values(map_values(
    if (.operationId | created) then
      .responses |= with_entries(if .key == "200" then .key = "201" | .value.description = "Created" else . end)
    elif (.operationId | deleted) then
      .responses |= (del(."200") + { "204": { description: "No Content" } })
    else . end
  ))
