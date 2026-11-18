class TokenBucket:
    def __init__(self, capacity, refill_per_second, clock):
        self.capacity = capacity
        self.rate = refill_per_second
        self.clock = clock
        self.level = 0
        self.last = clock()

    def tokens(self):
        return self.level

    def allow(self, cost=1):
        now = self.clock()
        self.level += (now - self.last) * self.rate
        self.last = now
        self.level -= cost
        return self.level >= 0
