import copy
import unittest
from unittest.mock import patch

from gpu_operator_versions.update_versions import (
    calculate_diffs,
    create_tests_matrix,
    filter_new_gpu_versions_by_catalog
)

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
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-main-latest': 'XYZ'})

    def test_bundle_changed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['gpu-main-latest'] = 'B'
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-main-latest': 'B'})

    def test_gpu_versions_key_created(self):
        old_versions = {}
        new_versions = {'gpu-operator': {'25.1': '25.1.1'}}
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-operator': {'25.1': '25.1.1'}})

    def test_gpu_version_changed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['gpu-operator']['25.1'] = '25.1.1'
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-operator': {'25.1': '25.1.1'}})

    def test_gpu_version_added(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['gpu-operator']['25.3'] = '25.3.0'
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'gpu-operator': {'25.3': '25.3.0'}})

    def test_gpu_version_removed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        del new_versions['gpu-operator']['25.2']
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {})

    def test_ocp_version_key_created(self):
        old_versions = {}
        new_versions = {'ocp': {'4.12': '4.12.2'}}
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'ocp': {'4.12': '4.12.2'}})

    def test_ocp_version_changed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['ocp']['4.12'] = '4.12.2'
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'ocp': {'4.12': '4.12.2'}})

    def test_ocp_version_added(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        new_versions['ocp']['4.15'] = '4.15.0'
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {'ocp': {'4.15': '4.15.0'}})

    def test_ocp_version_removed(self):
        old_versions = base_versions
        new_versions = copy.deepcopy(old_versions)
        del new_versions['ocp']['4.14']
        diff, _ = calculate_diffs(old_versions, new_versions)
        self.assertEqual(diff, {})


class TestCreateTestsMatrix(unittest.TestCase):

    def test_bundle_changed(self):
        diff = {'gpu-main-latest': 'B'}
        tests = create_tests_matrix(
            diff, ['4.14', '4.10', '4.11', '4.13'], ['21.3', '22.3'], default_support_matrix)
        self.assertEqual(tests, {('4.14', 'master', None), ('4.10', 'master', None)})

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
        self.assertEqual(tests, {('4.14', 'master', None), ('4.11', 'master', None)})

    def test_gpu_version_changed_existing(self):
        """When existing GPU version is updated, test only with active OCP versions."""
        diff = {'gpu-operator': {'25.1': '25.1.1'}}
        tests = create_tests_matrix(
            diff, ['4.11', '4.13'], ['24.3', '25.1'], default_support_matrix)
        # Should test with both OCP versions since both are active and use latest 2 GPU versions
        self.assertEqual(tests, {('4.11', '25.1', None), ('4.13', '25.1', None)})

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
        self.assertEqual(tests, {('4.13', '25.1', None)})

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
        self.assertEqual(tests, {('4.13', '25.3', None)})

    def test_ocp_version_changed_active(self):
        """When active OCP version gets patch, test with latest 2 GPU versions."""
        diff = {'ocp': {'4.12': '4.12.2'}}
        tests = create_tests_matrix(
            diff, ['4.12', '4.13'], ['24.4', '25.3'], default_support_matrix)
        self.assertEqual(tests, {('4.12', '24.4', None), ('4.12', '25.3', None)})

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
        self.assertEqual(tests, {('4.12', '25.3', None)})

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
        self.assertEqual(tests, {('4.12', '24.4', None), ('4.12', '25.3', None)})

    def test_ocp_version_added_new(self):
        """New OCP version defaults to active, tests with latest 2 GPU."""
        diff = {'ocp': {'4.15': '4.15.0'}}
        tests = create_tests_matrix(
            diff, ['4.12', '4.13', '4.15'], ['24.4', '25.3'], default_support_matrix)
        self.assertEqual(tests, {('4.15', '24.4', None), ('4.15', '25.3', None)})

    def test_no_changes(self):
        diff = {}
        tests = create_tests_matrix(
            diff, ['4.11', '4.13'], ['25.1', '25.3'], default_support_matrix)
        self.assertEqual(tests, set())


class TestFilterNewGpuVersionsByCatalog(unittest.TestCase):
    """Tests for catalog availability filtering."""

    def test_filters_out_unavailable_in_catalog(self):
        """Versions not in any catalog should be excluded."""
        gpu_diffs = {
            '25.3': '25.3.1',  # Not in catalog
            '25.2': '25.2.8',  # In catalog
        }
        ocp_versions = {'4.19': '4.19.0', '4.20': '4.20.0'}
        support_matrix = default_support_matrix

        # Mock catalog entries that show only 25.2 is available
        mock_entries = [
            {'version': '25.2.8', 'ocp_version': '4.19'},
            {'version': '25.2.8', 'ocp_version': '4.20'},
        ]

        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries',
                  return_value=mock_entries):
            filtered, _ = filter_new_gpu_versions_by_catalog(
                gpu_diffs, ocp_versions, support_matrix
            )

        # Only 25.2 should be included (available in catalog)
        self.assertEqual(filtered, {'25.2': '25.2.8'})

    def test_includes_all_versions_in_catalog(self):
        """All versions in catalog should be included regardless of minor version."""
        gpu_diffs = {
            '24.1': '24.1.5',  # Older version but in catalog
            '25.3': '25.3.1',  # Newer version and in catalog
            '25.2': '25.2.8',  # Another version in catalog
        }
        ocp_versions = {'4.19': '4.19.0', '4.20': '4.20.0'}
        support_matrix = default_support_matrix

        # Mock catalog entries showing all versions are available
        mock_entries = [
            {'version': '24.1.5', 'ocp_version': '4.19'},
            {'version': '25.3.1', 'ocp_version': '4.19'},
            {'version': '25.2.8', 'ocp_version': '4.20'},
        ]

        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries',
                  return_value=mock_entries):
            filtered, _ = filter_new_gpu_versions_by_catalog(
                gpu_diffs, ocp_versions, support_matrix
            )

        # All versions in catalog should be included
        self.assertEqual(filtered, {
            '24.1': '24.1.5',
            '25.3': '25.3.1',
            '25.2': '25.2.8',
        })

    def test_excludes_all_when_not_in_catalog(self):
        """Versions not in any catalog should all be excluded."""
        gpu_diffs = {
            '25.3': '25.3.1',  # Not in catalog
            '25.2': '25.2.8',  # Not in catalog
        }
        ocp_versions = {'4.19': '4.19.0', '4.20': '4.20.0'}
        support_matrix = default_support_matrix

        # Mock empty catalog entries
        mock_entries = []

        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries',
                  return_value=mock_entries):
            filtered, _ = filter_new_gpu_versions_by_catalog(
                gpu_diffs, ocp_versions, support_matrix
            )

        # Nothing should be included
        self.assertEqual(filtered, {})

    def test_available_in_at_least_one_ocp(self):
        """Version available in at least one OCP catalog should be included."""
        gpu_diffs = {
            '25.3': '25.3.1',
        }
        ocp_versions = {'4.19': '4.19.0', '4.20': '4.20.0'}
        support_matrix = default_support_matrix

        # Mock catalog showing version only in 4.20, not 4.19
        mock_entries = [
            {'version': '25.3.1', 'ocp_version': '4.20'},
        ]

        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries',
                  return_value=mock_entries):
            filtered, _ = filter_new_gpu_versions_by_catalog(
                gpu_diffs, ocp_versions, support_matrix
            )

        # Should be included since it's in at least one catalog
        self.assertEqual(filtered, {'25.3': '25.3.1'})

    def test_empty_gpu_diffs_returns_empty(self):
        """Empty diffs should return empty without calling catalog API."""
        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries') as mock_fetch:
            filtered, entries = filter_new_gpu_versions_by_catalog(
                {}, {'4.19': '4.19.0'}, default_support_matrix
            )
        self.assertEqual(filtered, {})
        self.assertEqual(entries, [])
        mock_fetch.assert_not_called()

    def test_no_active_ocp_returns_all_gpu_diffs(self):
        """When no active OCP versions exist, all GPU diffs should be kept."""
        gpu_diffs = {
            '25.3': '25.3.1',
            '25.2': '25.2.8',
        }
        ocp_versions = {'4.19': '4.19.0'}
        # Support matrix with only maintenance (no active) OCP versions
        support_matrix_no_active = {
            "openshift_support": {
                '4.19': {'status': 'maintenance', 'pinned_gpu_operator': '24.6'}
            }
        }

        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries') as mock_fetch:
            filtered, entries = filter_new_gpu_versions_by_catalog(
                gpu_diffs, ocp_versions, support_matrix_no_active
            )

        # All GPU diffs should be kept when there are no active OCPs
        self.assertEqual(filtered, gpu_diffs)
        self.assertEqual(entries, [])
        # Catalog API should not be called
        mock_fetch.assert_not_called()

    def test_catalog_filtering_in_calculate_diffs(self):
        """Catalog filtering should remove unavailable GPU versions from diffs."""
        old_versions = {
            'gpu-operator': {
                '25.1': '25.1.0',
                '25.2': '25.2.0'
            }
        }
        new_versions = {
            'gpu-operator': {
                '25.1': '25.1.0',
                '25.2': '25.2.0',
                '25.3': '25.3.1',  # New version, but not in catalog
                '25.4': '25.4.0',  # New version, in catalog
            }
        }
        ocp_versions = {'4.19': '4.19.0', '4.20': '4.20.0'}
        support_matrix = default_support_matrix

        # Mock catalog with only 25.4 available
        mock_entries = [
            {'version': '25.4.0', 'ocp_version': '4.19'},
            {'version': '25.4.0', 'ocp_version': '4.20'},
        ]

        with patch('gpu_operator_versions.update_versions.fetch_gpu_operator_catalog_entries',
                  return_value=mock_entries):
            diffs, _ = calculate_diffs(
                old_versions, new_versions, ocp_versions, support_matrix, check_catalog=True
            )

        # Only 25.4 should be in diffs (25.3 filtered out due to catalog unavailability)
        self.assertIn('gpu-operator', diffs)
        self.assertEqual(diffs['gpu-operator'], {'25.4': '25.4.0'})
        self.assertNotIn('25.3', diffs['gpu-operator'])


if __name__ == '__main__':
    unittest.main()
