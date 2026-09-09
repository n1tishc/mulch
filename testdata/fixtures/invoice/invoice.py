def total_cents(lines, discount_bps=0):
    return sum(round(line['unit_cents'] * line['quantity'] * (1-discount_bps/10000)) for line in lines)
