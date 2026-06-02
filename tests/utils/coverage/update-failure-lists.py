#!/usr/bin/env python3

# So basically what I need is
# - a history of failures
#   - if a test has always failed, including this run, then it should be run unconditionally
#   - if a test failed in the current run, then we should either use the results from the last pass or merge the two results
# - a core set of tests to run unconditionally
#   - this is generated on a weekly basis from feature tagging
#   - for resiliency sake, we should calculate from the past 3 runs, merging the results
# - coverage results
#   - if test passed, then simply include its results
#   - if a test failed, then either add it to the unconditional execution if it has always failed or grab the last version that passed (or merge these results with the last version that passed)
#   - if a test was aborted, then use historic data

# So what do I need to generate:
# - I need to have a document with the failures that have failed always. A failure should only be removed if it passes (and not if aborted)
# - Current results (the doc has sufficient information to merge/replace future results)
# - Check if I need to generate a new feature-tagging-based core set of features. I can base that on the most recent schedule run

# For failure list:
# - The always-on failures 