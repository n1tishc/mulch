def page(rows, limit, cursor=None):
    rows.sort(key=lambda row: row['ts'])
    start = 0 if cursor is None else cursor
    items = rows[start:start+limit]
    return {'items': items, 'next_cursor': start+limit if items else None}
