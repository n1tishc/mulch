# Rate limit contract v3 (current)
Implement limiter.TokenBucket(capacity, refill_per_second, clock).
capacity is an integer > 0. refill_per_second is an int or float > 0. Booleans and
strings are invalid for both; invalid arguments raise ValueError. clock is a
zero-argument callable returning seconds as a float.
A new bucket starts full. Tokens refill continuously at refill_per_second and never
exceed capacity. Refill is based on the latest clock reading seen so far: an earlier
reading adds no tokens and never moves that reading backwards.
allow(cost=1) returns True and consumes cost tokens only when at least cost tokens
are available; otherwise it returns False and consumes nothing. cost must be an
integer in [1, capacity] (booleans invalid), else ValueError, consuming nothing.
tokens() returns the current refilled token count as a float without consuming.
Implement middleware.Limiter(capacity, refill_per_second, clock) whose
handle(client_id, cost=1) -> bool uses one independent bucket per client_id, created
full on that client's first request. Clients never share tokens.
Use only Python's standard library. Keep existing class and method signatures.
