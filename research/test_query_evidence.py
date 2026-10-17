import unittest
from query_evidence import analyze_quotas


class QuotaEvidenceTests(unittest.TestCase):
    def test_auxiliary_500_and_business_status_two_do_not_hide_valid_login(self):
        main = {"quotainfo": {"status": 200, "body": {"remainQuota": 18, "totalQuota": 20}},
                "planCreditInfo": {"status": 500}}
        studio = {"get_remaining_times": {"status": 200, "body": {"errNo": 0, "data": {"remainingTimes": 29}}},
                  "getuserinfo": {"status": 200, "body": {"status": 2}}}
        evidence = analyze_quotas(main, studio)
        self.assertTrue(evidence["primary_quotas_verified"])
        self.assertEqual(evidence["main_remaining"], 18)
        self.assertEqual(evidence["studio_remaining"], 29)
        self.assertEqual(evidence["auxiliary_failures"], ["main.planCreditInfo"])

    def test_unauthorized_primary_is_not_an_empty_success(self):
        evidence = analyze_quotas({"quotainfo": {"status": 401}},
                                  {"get_remaining_times": {"status": 401}})
        self.assertFalse(evidence["primary_quotas_verified"])

    def test_http_200_with_business_failure_is_not_verified(self):
        evidence = analyze_quotas({"quotainfo": {"status": 200, "body": {"errNo": 7, "remainQuota": 18}}},
                                  {"get_remaining_times": {"status": 200, "body": {"remainingTimes": 29}}})
        self.assertFalse(evidence["primary_quotas_verified"])


if __name__ == "__main__":
    unittest.main()
