"""Fail closed on skipped, stale, incomplete or mismatched signed-phase results."""

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

from test_validate_signed_request import IMAGE

spec = importlib.util.spec_from_file_location('precompiled_result', Path(__file__).with_name(
    'verify-precompiled-step-result.py'))
result = importlib.util.module_from_spec(spec)
spec.loader.exec_module(result)
STEP = 'network-operator-e2e-precompiled'
WORKERS = ['worker1', 'worker2']
REPORTS = [dict(step=step, attempt='current', status='passed', nno_csv_version='26.7.0-1',
                precompiled_selection={'pull_spec': IMAGE}, checks=dict.fromkeys(checks, 'passed'))
           for step, checks in [(STEP, ['requested_driver_policy', 'requested_driver_running', 'nic_cluster_policy_ready']),
                                (STEP + '-rdma', ['rdma_shared_device'])]]
EVIDENCE = dict(step=STEP, attempt='current', pull_spec=IMAGE,
                workers=dict.fromkeys(WORKERS, 'sha256:' + 'a' * 64))


class ResultTests(unittest.TestCase):
    def verify(self, reports=REPORTS, evidence=EVIDENCE):
        """Write report fixtures and validate the current attempt's driver evidence."""
        with tempfile.TemporaryDirectory() as directory:
            for index, report in enumerate(reports):
                (Path(directory) / f'nno-step-result-{index}.json').write_text(json.dumps(report))
            (Path(directory) / 'nno-signed-driver-images.json').write_text(json.dumps(evidence))
            result.verify(directory, IMAGE, 'current', WORKERS)

    def test_complete_results_pass(self):
        """Accept matching deploy, RDMA and worker evidence for this attempt."""
        self.verify()

    def test_missing_or_stale_rdma_result_fails(self):
        """Reject absent deployment/RDMA results and reports from another attempt."""
        for reports in [[], REPORTS[:1]]:
            with self.assertRaises(ValueError):
                self.verify(reports)
        reports = copy.deepcopy(REPORTS)
        reports[1]['attempt'] = 'previous'
        with self.assertRaises(ValueError):
            self.verify(reports)

    def test_skips_wrong_driver_family_and_incomplete_checks_fail(self):
        """Reject skipped suites, wrong images/families and incomplete checks."""
        for index in range(2):
            for field, value in [('status', 'skipped'), ('checks', {}),
                                 ('precompiled_selection', {'pull_spec': 'other-image'}),
                                 ('nno_csv_version', '25.10.0')]:
                reports = copy.deepcopy(REPORTS)
                reports[index][field] = value
                with self.subTest(index=index, field=field), self.assertRaises(ValueError):
                    self.verify(reports)

    def test_wrong_attempt_image_or_missing_worker_evidence_fails(self):
        """Reject stale driver evidence and missing image IDs for either worker."""
        for field, value in [('attempt', 'previous'), ('pull_spec', 'other-image'),
                             ('workers', {'worker1': 'sha256:' + 'a' * 64}),
                             ('workers', dict.fromkeys(WORKERS, ''))]:
            evidence = copy.deepcopy(EVIDENCE)
            evidence[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.verify(evidence=evidence)


if __name__ == '__main__':
    unittest.main()
