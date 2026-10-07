GET {{base_url}}/state
HTTP 200
[Asserts]
jsonpath "$.grants" == 1
jsonpath "$.status" == "paid"
jsonpath "$.version" == 3
