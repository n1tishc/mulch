# Settings contract v2 (current)
Implement merge.deep_merge(base, override) returning a NEW dict:
- a key present in only one input is copied verbatim;
- when both values are dicts, they are merged recursively by these same rules;
- otherwise the override value replaces the base value; lists are replaced, never
  concatenated, and a dict may replace a scalar or vice versa;
- an override value of None removes that key from the result;
- neither input, nor any dict or list nested inside them, is mutated or shared with
  the result.
Implement pairs.parse(lines). Each line is "dotted.key=value". Skip blank lines and
lines whose first non-space character is "#". Split on the FIRST "=" only and strip
surrounding whitespace from the key and the value. "a.b.c=1" yields
{"a": {"b": {"c": 1}}}. Values "true"/"false" (any case) become booleans, "null"
becomes None, an optional sign followed only by digits becomes an int, and anything
else stays a string. Later lines win: a later line may replace an earlier scalar with
a nested dict, or an earlier dict with a scalar. A line with no "=", an empty key, or
an empty key segment raises ValueError whose message contains "line N" for its
1-based line number.
Implement settings.resolve(defaults, layers, lines): start from defaults, deep_merge
each layer in order, then deep_merge parse(lines) last, so lines have the highest
precedence. resolve must not mutate or share any input.
Use only Python's standard library. Keep existing function signatures.
