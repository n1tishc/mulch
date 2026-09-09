import unittest
from events import page
from export import all_rows

class PublicTests(unittest.TestCase):
    def test_first(self):
        self.assertEqual(page([{'id': 1, 'ts': 10}], 2)['items'], [{'id': 1, 'ts': 10}])
    def test_export(self):
        rows = [{'id': i, 'ts': 10} for i in range(3)]
        self.assertEqual(len(all_rows(rows, 1)), 3)

if __name__ == '__main__':
    unittest.main()
