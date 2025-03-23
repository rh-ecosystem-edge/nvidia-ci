import copy
import unittest
from utils import calculate_diffs, create_tests_matrix

base_versions = {
    'gpu-main-latest': 'A',
    'gpu-operator': {
        '25.1': '25.1.0',
        '25.2': '25.2.0'
    },
    'ocp': {
        '4.12': '4.12.1',
        '4.14': '4.14.1'
    }
}


class TestCalculateDiffs(unittest.TestCase):

    def test_bundle_key_created(self):
        old_versions = {}
        new_versions = {'gpu-main-latest': 'XYZ'}
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-main-latest': 'XYZ'})

    def test_bundle_changed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['gpu-main-latest'] = 'B'
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-main-latest': 'B'})

    def test_gpu_versions_key_created(self):
        old_versions = {}
        new_versions = {'gpu-operator': {'25.1': '25.1.1'}}
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-operator': {'25.1': '25.1.1'}})

    def test_gpu_version_changed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['gpu-operator']['25.1'] = '25.1.1'
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-operator': {'25.1': '25.1.1'}})

    def test_gpu_version_added(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['gpu-operator']['25.3'] = '25.3.0'
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-operator': {'25.3': '25.3.0'}})

    def test_gpu_version_removed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        del new_versions['gpu-operator']['25.2']
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {})

    def test_ocp_version_key_created(self):
        old_versions = {}
        new_versions = {'ocp': {'4.12': '4.12.2'}}
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'ocp': {'4.12': '4.12.2'}})

    def test_ocp_version_changed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['ocp']['4.12'] = '4.12.2'
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'ocp': {'4.12': '4.12.2'}})

    def test_ocp_version_added(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['ocp']['4.15'] = '4.15.0'
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'ocp': {'4.15': '4.15.0'}})

    def test_ocp_version_removed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        del new_versions['ocp']['4.14']
        diff = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {})


class TestCreateTestsMatrix(unittest.TestCase):

    def test_bundle_changed(self):
        diff = {'gpu-main-latest': 'B'}
        tests = create_tests_matrix(
            diff, ['4.14', '4.10', '4.11', '4.13'], ['21.3', '22.3'])
        self.assertEqual(tests, {('4.14', 'master'), ('4.10', 'master')})

    def test_gpu_version_changed(self):
        diff = {'gpu-operator': {'25.1': '25.1.1'}}
        tests = create_tests_matrix(diff, ['4.11', '4.13'], ['24.3', '25.1'])
        self.assertEqual(tests, {('4.11', '25.1'), ('4.13', '25.1')})

    def test_gpu_version_added(self):
        diff = {'gpu-operator': {'25.3': '25.3.0'}}
        tests = create_tests_matrix(diff, ['4.11', '4.13'], ['25.1', '25.3'])
        self.assertEqual(tests, {('4.11', '25.3'), ('4.13', '25.3')})

    def test_ocp_version_changed(self):
        diff = {'ocp': {'4.12': '4.12.2'}}
        tests = create_tests_matrix(diff, ['4.12', '4.13'], ['24.4', '25.3'])
        self.assertEqual(tests, {('4.12', '24.4'), ('4.12', '25.3')})

    def test_ocp_version_added(self):
        diff = {'ocp': {'4.15': '4.15.0'}}
        tests = create_tests_matrix(
            diff, ['4.12', '4.13', '4.15'], ['24.4', '25.3'])
        self.assertEqual(tests, {('4.15', '24.4'), ('4.15', '25.3')})

    def test_no_changes(self):
        diff = {}
        tests = create_tests_matrix(diff, ['4.11', '4.13'], ['25.1', '25.3'])
        self.assertEqual(tests, set())


if __name__ == '__main__':
    unittest.main()
