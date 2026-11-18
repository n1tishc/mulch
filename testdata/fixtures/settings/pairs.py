def parse(lines):
    result = {}
    for line in lines:
        if not line.strip():
            continue
        key, value = line.split('=')
        node = result
        parts = key.split('.')
        for part in parts[:-1]:
            node = node.setdefault(part, {})
        node[parts[-1]] = int(value) if value.isdigit() else value
    return result
