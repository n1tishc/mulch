import unittest
from invoice import total_cents
from checkout import checkout_total

class PublicTests(unittest.TestCase):
    def test_basic(self):
        self.assertEqual(total_cents([{'unit_cents': 100, 'quantity': 2}]), 200)
    def test_checkout(self):
        self.assertEqual(checkout_total([{'unit_cents': 100, 'quantity': 2}], 5000), 100)

if __name__ == '__main__':
    unittest.main()
