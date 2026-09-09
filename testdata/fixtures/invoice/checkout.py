from invoice import total_cents

def checkout_total(lines, discount_bps=0):
    return total_cents(lines) - discount_bps
