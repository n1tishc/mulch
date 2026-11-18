import pathlib, sys
sys.path.insert(0, str(pathlib.Path(sys.argv[1]).resolve()))
from limiter import TokenBucket
from middleware import Limiter


class Clock:
    def __init__(self):
        self.now = 100.0

    def __call__(self):
        return self.now


checks = 0


def check(condition, label):
    global checks
    assert condition, label
    checks += 1


def near(a, b):
    return abs(a - b) < 1e-9


clock = Clock()
bucket = TokenBucket(3, 0.5, clock)
check(near(bucket.tokens(), 3), 'new bucket starts full')
check(bucket.allow(2) and near(bucket.tokens(), 1), 'allowed request consumes its cost')
check(not bucket.allow(2) and near(bucket.tokens(), 1), 'denied request consumes nothing')
clock.now += 2
check(near(bucket.tokens(), 2), 'refill is continuous')
check(near(bucket.tokens(), 2), 'tokens() does not consume')
clock.now += 100
check(near(bucket.tokens(), 3), 'refill is capped at capacity')
check(bucket.allow(3) and not bucket.allow(1), 'a full drain empties the bucket')
latest = clock.now
clock.now = latest - 50
check(near(bucket.tokens(), 0) and not bucket.allow(1), 'an earlier clock reading adds nothing')
clock.now = latest - 10
check(near(bucket.tokens(), 0), 'refill does not restart from an earlier reading')
clock.now = latest + 2
check(near(bucket.tokens(), 1), 'refill resumes from the latest reading')

for args in [(0, 1), (-1, 1), (True, 1), (1.5, 1), ('2', 1), (2, 0), (2, -1), (2, True), (2, '1')]:
    try:
        TokenBucket(args[0], args[1], clock)
    except ValueError:
        checks += 1
    else:
        raise AssertionError(('accepted invalid bucket', args))
fresh = TokenBucket(2, 1, clock)
for cost in [0, -1, 3, True, 1.5, '1']:
    try:
        fresh.allow(cost)
    except ValueError:
        checks += 1
    else:
        raise AssertionError(('accepted invalid cost', cost))
check(near(fresh.tokens(), 2), 'an invalid cost consumes nothing')
check(near(TokenBucket(4, 2.5, clock).tokens(), 4), 'float refill rates are valid')

clock = Clock()
limiter = Limiter(2, 1, clock)
check(limiter.handle('a', 2), 'client a drains its own bucket')
check(not limiter.handle('a'), 'client a is limited')
check(limiter.handle('b', 2), 'client b has an independent full bucket')
clock.now += 1
check(limiter.handle('a') and not limiter.handle('a'), 'client a refills independently')
check(not limiter.handle('b', 2) and limiter.handle('b'), 'client b refills independently')
print(f'PASS {checks} hidden assertions')
