"""Private UI harness completeness; process exit is not an acceptance result."""
def qualifies(rows, expected):
    return bool(expected) and len(set(expected)) == len(expected) and [r.get('name') for r in rows] == expected and all(r.get('status') == 'PASS' for r in rows)
