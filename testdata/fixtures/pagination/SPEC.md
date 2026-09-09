# Event pagination contract v2 (current)
Implement events.page(rows, limit, cursor=None).
Rows have unique integer id and integer timestamp ts; input need not be sorted.
Order ascending by (ts,id). A cursor is an exclusive (ts,id) lower bound, even if
that row has since been deleted. Include rows strictly greater than the cursor.
Return {'items': [rows...], 'next_cursor': (ts,id) or None}.
next_cursor is the last returned row's pair ONLY when more eligible rows exist.
limit must be an integer > 0; reject booleans and invalid limits with ValueError.
Do not sort/mutate the caller's list. Returned row dictionaries must be independent
copies: changing a result must not change the source. Empty pages have no cursor.
Implement export.all_rows(rows, batch_size) to collect all pages using this API.
Use only Python's standard library. Keep existing function signatures.
