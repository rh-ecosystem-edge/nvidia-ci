import unittest
from utils import get_latest_versions, get_earliest_versions

class TestGetLatestVersions(unittest.TestCase):

    def test_empty(self):
        versions = []
        self.assertEqual(get_latest_versions(versions, 2), [])

    def test_less_than_count(self):
        versions = ['1.1']
        self.assertEqual(get_latest_versions(versions, 2), ['1.1'])

    def test_exact_count(self):
        versions = ['1.1', '1.2']
        self.assertEqual(get_latest_versions(versions, 2), ['1.1', '1.2'])

    def test_more_than_count(self):
        versions = ['1.1', '1.3', '1.2']
        self.assertEqual(get_latest_versions(versions, 2), ['1.2', '1.3'])

    def test_count_one(self):
        versions = ['1.1', '1.3', '1.2']
        self.assertEqual(get_latest_versions(versions, 1), ['1.3'])

    def test_reverse_order(self):
        versions = ['1.3', '1.2', '1.1']
        self.assertEqual(get_latest_versions(versions, 3), ['1.1', '1.2', '1.3'])

class TestGetEarliestVersions(unittest.TestCase):

    def test_empty(self):
        versions = []
        self.assertEqual(get_earliest_versions(versions, 2), [])

    def test_less_than_count(self):
        versions = ['1.1']
        self.assertEqual(get_earliest_versions(versions, 2), ['1.1'])

    def test_exact_count(self):
        versions = ['1.2', '1.1']
        self.assertEqual(get_earliest_versions(versions, 2), ['1.1', '1.2'])

    def test_more_than_count(self):
        versions = ['1.2', '1.3', '1.1']
        self.assertEqual(get_earliest_versions(versions, 2), ['1.1', '1.2'])

    def test_count_one(self):
        versions = ['1.3', '1.1', '1.2']
        self.assertEqual(get_earliest_versions(versions, 1), ['1.1'])

    def test_reverse_order(self):
        versions = ['1.3', '1.2', '1.1']
        self.assertEqual(get_earliest_versions(versions, 3), ['1.1', '1.2', '1.3'])


if __name__ == '__main__':
    unittest.main()
