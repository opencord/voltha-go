#System-Tests

Automated test-suites to validate the stability/functionality of VOLTHA. Tests that reside in here should be written in Robot Framework and Python.

Intended use includes:

* Sanity testing
* Regression testing
* Acceptance testing
* Functional testing
* Scale Testing using BBSIM
* Failure/Scenario testing

 
Directory Structures are as followed:
* `tests/` - Test Suites that consist of multiple test cases. Test cases should be grouped together with common testing purposes. Eg. sanity, functionality, controlplane, dataplane, failover, scale, etc. 
* `libraries/` - [Common test keywords](https://robotframework.org/robotframework/latest/RobotFrameworkUserGuide.html#using-test-libraries). Files in here should contain common/generic operations of the VOLTHA Controller that can be utilized across multiple test suites. These libraries can also be created in Python.  

 
##Getting Started
1. Create test virtual-environment
    * `source setup_venv.sh`
2. Run Test-Suites
    * eg. `robot -V tests/data/voltha_minimal_k8_resources.yaml tests/sanity_k8s.robot`
    
This test execution will generate three report files (`output.xml`, `report.html`, `log.html`). View the `report.html` page to analyze the results. 

## WIP:
*  Containerizing test environment so these tests can be run independent of the system. 