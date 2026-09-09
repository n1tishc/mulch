# Invoice contract v2 (current)
Implement invoice.total_cents(lines, discount_bps=0).
Each line is a dict with integer unit_cents >= 0 and integer quantity >= 0.
Reject booleans and non-integers as well as negative values with ValueError.
discount_bps is an integer in [0,10000]; booleans are invalid.
Compute the full subtotal in integer cents, apply the discount ONCE to that subtotal,
then round the result to the nearest integer cent, with halves rounded UP.
An empty invoice returns 0, but still validates the discount.
Never mutate the input lines. Update checkout.checkout_total to use this contract.
Use only the Python standard library. Public functions and signatures must remain.
