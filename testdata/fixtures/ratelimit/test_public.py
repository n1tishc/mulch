import unittest
from limiter import TokenBucket
from middleware import Limiter


class Clock:
    def __init__(self):
        self.now = 0.0

    def __call__(self):
        return self.now


class PublicTests(unittest.TestCase):
    def test_refill(self):
        clock = Clock()
        bucket = TokenBucket(2, 1, clock)
        clock.now = 5
        self.assertTrue(bucket.allow())

    def test_middleware(self):
        clock = Clock()
        limiter = Limiter(1, 1, clock)
        clock.now = 1
        self.assertTrue(limiter.handle('a'))


    def test_starts_full_and_denies_overdraw(self):
        clock = Clock()
        bucket = TokenBucket(1, 1, clock)
        self.assertTrue(bucket.allow())
        self.assertFalse(bucket.allow())


if __name__ == '__main__':
    unittest.main()
