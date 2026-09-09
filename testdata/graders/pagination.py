import copy, pathlib, sys
sys.path.insert(0, str(pathlib.Path(sys.argv[1]).resolve()))
from events import page
from export import all_rows
rows = [{'id': 5, 'ts': 20}, {'id': 3, 'ts': 10}, {'id': 1, 'ts': 10}, {'id': 4, 'ts': 20}]
original = copy.deepcopy(rows)
expected = sorted(rows, key=lambda row: (row['ts'], row['id']))
checks = 0
for limit in [1, 2, 3, 4, 10]:
    for cursor in [None, (10,1), (10,2), (15,9), (20,5), (30,0)]:
        eligible = [r for r in expected if cursor is None or (r['ts'],r['id']) > cursor]
        result = page(rows, limit, cursor)
        assert result['items'] == eligible[:limit], (limit, cursor, result)
        want = (eligible[limit-1]['ts'],eligible[limit-1]['id']) if len(eligible)>limit else None
        assert result['next_cursor'] == want, (result, want)
        assert rows == original, 'mutated input'
        if result['items']:
            result['items'][0]['ts'] = -100
            assert rows == original, 'returned a borrowed row'
        checks += 4
    assert all_rows(rows, limit) == expected
    checks += 1
assert page([],1) == {'items': [], 'next_cursor': None}
for invalid in [0, -1, True, 1.5, '2', None]:
    try: page(rows, invalid)
    except ValueError: checks += 1
    else: raise AssertionError(('accepted invalid limit', invalid))
print(f'PASS {checks+1} hidden assertions')
