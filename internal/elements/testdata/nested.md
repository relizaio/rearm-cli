# Cycle refusal

Some prose that is not an element.

## REQ-1 The board refuses a dependency cycle
level: 1

A cycle stalls every task in it.

### REQ-1.1 The refusal names every task in the cycle
traces: derives_from CONOPS-3; is_verified_by TEST-7, TEST-8
assumes: ADR-2

#### Notes

Still REQ-1.1's content.

#### REQ-1.1.a Names are the tasks' refs
parent: REQ-1

## Design notes

### FN-1 Detect cycles on authorize
traces: satisfies REQ-1
