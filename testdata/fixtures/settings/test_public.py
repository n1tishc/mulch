import unittest
from merge import deep_merge
from pairs import parse
from settings import resolve


class PublicTests(unittest.TestCase):
    def test_merge(self):
        self.assertEqual(deep_merge({'a': 1}, {'b': 2}), {'a': 1, 'b': 2})

    def test_parse(self):
        self.assertEqual(parse(['db.port=5432']), {'db': {'port': 5432}})

    def test_resolve(self):
        self.assertEqual(resolve({'a': 1}, [{'b': 2}], []), {'a': 1, 'b': 2})


    def test_lines_override_layers(self):
        self.assertEqual(resolve({'a': 1}, [{'a': 2}], ['a=3']), {'a': 3})


if __name__ == '__main__':
    unittest.main()
