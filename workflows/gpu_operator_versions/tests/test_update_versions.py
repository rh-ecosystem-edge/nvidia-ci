import copy
import unittest

from workflows.gpu_operator_versions.update_versions import calculate_diffs, create_tests_matrix

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

default_support_matrix = {
    "openshift_support": {},
    "defaults": {
        "unlisted_versions": {
            "status": "active"
        }
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
            diff, ['4.14', '4.10', '4.11', '4.13'], ['21.3', '22.3'], default_support_matrix)
        self.assertEqual(tests, {('4.14', 'master'), ('4.10', 'master')})

    def test_bundle_changed_with_maintenance(self):
        """Master should only test with active OCP versions."""
        diff = {'gpu-main-latest': 'B'}
        support_matrix = {
            "openshift_support": {
                "4.10": {"status": "maintenance", "pinned_gpu_operator": ["21.3"]},
                "4.11": {"status": "active"}
            },
            "defaults": {"unlisted_versions": {"status": "active"}}
        }
        tests = create_tests_matrix(
            diff, ['4.14', '4.10', '4.11', '4.13'], ['21.3', '22.3'], support_matrix)
        # Should only test with active versions: 4.14 (newest), 4.11 (oldest active)
        self.assertEqual(tests, {('4.14', 'master'), ('4.11', 'master')})

    def test_gpu_version_changed_existing(self):
        """When existing GPU version is updated, test only with active OCP versions."""
        diff = {'gpu-operator': {'25.1': '25.1.1'}}
        tests = create_tests_matrix(
            diff, ['4.11', '4.13'], ['24.3', '25.1'], default_support_matrix)
        # Should test with both OCP versions since both are active and use latest 2 GPU versions
        self.assertEqual(tests, {('4.11', '25.1'), ('4.13', '25.1')})

    def test_gpu_version_changed_with_maintenance(self):
        """When existing GPU version is updated, maintenance OCP versions should NOT test it."""
        diff = {'gpu-operator': {'25.1': '25.1.1'}}
        support_matrix = {
            "openshift_support": {
                "4.11": {"status": "maintenance", "pinned_gpu_operator": ["25.1"]}
            },
            "defaults": {"unlisted_versions": {"status": "active"}}
        }
        tests = create_tests_matrix(
            diff, ['4.11', '4.13'], ['25.1', '25.3'], support_matrix)
        # Should only test with active OCP (4.13), NOT maintenance (4.11) even though 25.1 is pinned
        self.assertEqual(tests, {('4.13', '25.1')})

    def test_gpu_version_added_new(self):
        """When new GPU version is added, test with active OCP versions only."""
        diff = {'gpu-operator': {'25.3': '25.3.0'}}
        support_matrix = {
            "openshift_support": {
                "4.11": {"status": "maintenance", "pinned_gpu_operator": ["25.1"]}
            },
            "defaults": {"unlisted_versions": {"status": "active"}}
        }
        tests = create_tests_matrix(
            diff, ['4.11', '4.13'], ['25.1', '25.3'], support_matrix)
        # Should only test with active OCP (4.13), not maintenance (4.11)
        self.assertEqual(tests, {('4.13', '25.3')})

    def test_ocp_version_changed_active(self):
        """When active OCP version gets patch, test with latest 2 GPU versions."""
        diff = {'ocp': {'4.12': '4.12.2'}}
        tests = create_tests_matrix(
            diff, ['4.12', '4.13'], ['24.4', '25.3'], default_support_matrix)
        self.assertEqual(tests, {('4.12', '24.4'), ('4.12', '25.3')})

    def test_ocp_version_changed_maintenance(self):
        """When maintenance OCP version gets patch, test only with pinned GPU."""
        diff = {'ocp': {'4.12': '4.12.2'}}
        support_matrix = {
            "openshift_support": {
                "4.12": {"status": "maintenance", "pinned_gpu_operator": ["25.3"]}
            },
            "defaults": {"unlisted_versions": {"status": "active"}}
        }
        tests = create_tests_matrix(
            diff, ['4.12', '4.13'], ['24.4', '25.3'], support_matrix)
        # Should only test with pinned version
        self.assertEqual(tests, {('4.12', '25.3')})

    def test_ocp_version_changed_maintenance_multiple_pins(self):
        """Test maintenance OCP with multiple pinned GPU versions."""
        diff = {'ocp': {'4.12': '4.12.2'}}
        support_matrix = {
            "openshift_support": {
                "4.12": {"status": "maintenance", "pinned_gpu_operator": ["24.4", "25.3"]}
            },
            "defaults": {"unlisted_versions": {"status": "active"}}
        }
        tests = create_tests_matrix(
            diff, ['4.12', '4.13'], ['24.4', '25.3'], support_matrix)
        # Should test with both pinned versions
        self.assertEqual(tests, {('4.12', '24.4'), ('4.12', '25.3')})

    def test_ocp_version_added_new(self):
        """New OCP version defaults to active, tests with latest 2 GPU."""
        diff = {'ocp': {'4.15': '4.15.0'}}
        tests = create_tests_matrix(
            diff, ['4.12', '4.13', '4.15'], ['24.4', '25.3'], default_support_matrix)
        self.assertEqual(tests, {('4.15', '24.4'), ('4.15', '25.3')})

    def test_no_changes(self):
        diff = {}
        tests = create_tests_matrix(
            diff, ['4.11', '4.13'], ['25.1', '25.3'], default_support_matrix)
        self.assertEqual(tests, set())


if __name__ == '__main__':
    unittest.main()
