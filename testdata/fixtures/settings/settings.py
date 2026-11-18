from merge import deep_merge
from pairs import parse


def resolve(defaults, layers, lines):
    result = defaults
    for layer in layers:
        result = deep_merge(result, layer)
    return deep_merge(parse(lines), result)
