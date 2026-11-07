import unittest
from acceptance import qualifies
class Completeness(unittest.TestCase):
 def test_complete(self):self.assertTrue(qualifies([{'name':'a','status':'PASS'},{'name':'b','status':'PASS'}],['a','b']))
 def test_partial_pass_is_not_success(self):self.assertFalse(qualifies([{'name':'a','status':'PASS'}],['a','b']))
 def test_duplicate_and_failed(self):
  self.assertFalse(qualifies([{'name':'a','status':'PASS'},{'name':'a','status':'PASS'}],['a','b']))
  self.assertFalse(qualifies([{'name':'a','status':'FAIL'}],['a']))
 def test_empty(self):self.assertFalse(qualifies([],[]))
