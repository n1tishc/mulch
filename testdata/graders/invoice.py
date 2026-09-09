import copy, importlib, pathlib, sys
sys.path.insert(0, str(pathlib.Path(sys.argv[1]).resolve()))
from invoice import total_cents
from checkout import checkout_total
checks = 0

def equal(actual, expected):
    global checks
    assert actual == expected, (actual, expected)
    checks += 1

for fn in [total_cents, checkout_total]:
    for units, quantity, discount in [(1, 1, 5000), (1, 3, 5000), (101, 3, 3333), (10**18+1, 3, 9999), (0, 3, 0)]:
        lines = [{'unit_cents': units, 'quantity': quantity}]
        before = copy.deepcopy(lines)
        expected = (units*quantity*(10000-discount)+5000)//10000
        equal(fn(lines, discount), expected)
        equal(lines, before)
    equal(fn([{'unit_cents': 1, 'quantity': 1}]*2, 5000), 1)
    equal(fn([], 10000), 0)
    for field in ['unit_cents', 'quantity']:
        for invalid in [-1, 1.5, True, '2', None]:
            line = {'unit_cents': 3, 'quantity': 2}
            line[field] = invalid
            try: fn([line])
            except ValueError: checks += 1
            else: raise AssertionError(('accepted invalid line', field, invalid))
    for invalid in [-1, 10001, 0.5, True, '1']:
        try: fn([], invalid)
        except ValueError: checks += 1
        else: raise AssertionError(('accepted invalid discount', invalid))
print(f'PASS {checks} hidden assertions')
