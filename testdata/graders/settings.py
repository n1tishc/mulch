import copy, pathlib, sys
sys.path.insert(0, str(pathlib.Path(sys.argv[1]).resolve()))
from merge import deep_merge
from pairs import parse
from settings import resolve

checks = 0


def check(condition, label):
    global checks
    assert condition, label
    checks += 1


base = {'db': {'host': 'a', 'port': 1, 'opts': {'ssl': True}}, 'tags': ['x'], 'keep': 1, 'drop': 2}
override = {'db': {'port': 2, 'opts': {'timeout': 5}}, 'tags': ['y'], 'drop': None, 'new': {'n': [1]}}
base_copy, override_copy = copy.deepcopy(base), copy.deepcopy(override)
merged = deep_merge(base, override)
check(merged == {'db': {'host': 'a', 'port': 2, 'opts': {'ssl': True, 'timeout': 5}}, 'tags': ['y'], 'keep': 1, 'new': {'n': [1]}}, ('merge', merged))
check(base == base_copy and override == override_copy, 'deep_merge mutated an input')
merged['db']['opts']['ssl'] = False
merged['tags'].append('z')
merged['new']['n'].append(2)
merged['db']['host'] = 'changed'
check(base == base_copy and override == override_copy, 'deep_merge shares nested objects with an input')
check(deep_merge({'a': {'b': 1}}, {'a': 3}) == {'a': 3}, 'a scalar replaces a dict')
replacement = {'b': [1]}
result = deep_merge({'a': 3}, {'a': replacement})
check(result == {'a': {'b': [1]}}, 'a dict replaces a scalar')
result['a']['b'].append(2)
check(replacement == {'b': [1]}, 'a replacing dict is copied')
check(deep_merge({}, {'gone': None}) == {}, 'None for a missing key adds nothing')
check(deep_merge({'a': {'b': 1, 'c': 2}}, {'a': {'b': None}}) == {'a': {'c': 2}}, 'nested None removes a key')
check(deep_merge({'a': [1, 2]}, {'a': []}) == {'a': []}, 'an empty list replaces a list')

lines = ['# comment', '', '   # indented comment', '  db.host = example.com ', 'db.port=5432',
         'db.url=postgres://u:p@h/db?x=1', 'flags.debug=TRUE', 'flags.beta=false', 'limit=-3',
         'offset=+4', 'ratio=1.5', 'owner=null', 'code=007', 'label=  spaced value  ']
want = {'db': {'host': 'example.com', 'port': 5432, 'url': 'postgres://u:p@h/db?x=1'},
        'flags': {'debug': True, 'beta': False}, 'limit': -3, 'offset': 4, 'ratio': '1.5',
        'owner': None, 'code': 7, 'label': 'spaced value'}
parsed = parse(lines)
check(parsed == want, ('parse', parsed))
check(type(parsed['flags']['debug']) is bool and type(parsed['db']['port']) is int, 'value types')
check(parse(['a=1', 'a.b=2']) == {'a': {'b': 2}}, 'a later nested line replaces a scalar')
check(parse(['a.b=2', 'a=1']) == {'a': 1}, 'a later scalar replaces a dict')
check(parse(['x=1-2', 'y=--3', 'z=TrUe']) == {'x': '1-2', 'y': '--3', 'z': True}, 'only signed digit strings are ints')
for bad, number in [(['ok=1', 'novalue'], 2), (['=1'], 1), (['a..b=1'], 1), (['a.=1'], 1), (['ok=1', '', '.a=1'], 3)]:
    try:
        parse(bad)
    except ValueError as error:
        check(f'line {number}' in str(error), ('error must name the line', bad, str(error)))
    else:
        raise AssertionError(('accepted invalid lines', bad))

defaults = {'db': {'host': 'local', 'port': 1}, 'mode': 'dev', 'tags': ['base']}
layers = [{'db': {'port': 2}, 'tags': ['layer']}, {'mode': 'prod', 'db': {'host': None}}]
defaults_copy, layers_copy = copy.deepcopy(defaults), copy.deepcopy(layers)
resolved = resolve(defaults, layers, ['db.port=3', 'extra=yes', 'mode=staging'])
check(resolved == {'db': {'port': 3}, 'mode': 'staging', 'tags': ['layer'], 'extra': 'yes'}, ('resolve', resolved))
check(defaults == defaults_copy and layers == layers_copy, 'resolve mutated an input')
resolved['db']['port'] = 99
resolved['tags'].append('x')
check(defaults == defaults_copy and layers == layers_copy, 'resolve shares nested objects with an input')
check(resolve({'a': 1}, [], []) == {'a': 1}, 'no layers and no lines')
check(resolve({}, [{'a': {'b': 1}}], ['a.b=2']) == {'a': {'b': 2}}, 'lines have the highest precedence')
print(f'PASS {checks} hidden assertions')
