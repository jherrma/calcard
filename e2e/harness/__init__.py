"""Support code for the CalCard end-to-end suite (black-box: HTTP only).

Nothing in here imports server code or decodes responses into typed models:
tests assert on the raw status, headers, bytes and the plain ``json.loads``
result of each response, so an unexpected field (e.g. a leaked password hash)
is always visible to them.
"""
