from limiter import TokenBucket


class Limiter:
    def __init__(self, capacity, refill_per_second, clock):
        self.bucket = TokenBucket(capacity, refill_per_second, clock)

    def handle(self, client_id, cost=1):
        return self.bucket.allow(cost)
