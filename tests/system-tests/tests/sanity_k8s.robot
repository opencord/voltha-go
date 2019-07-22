*** Settings ***
Documentation     Validates K8s environment during post-install
Library           OperatingSystem
Resource          ../libraries/utils.robot

*** Test Cases ***
Validate Helm Charts
    [Documentation]    Validates that all the expected helm-charts are in DEPLOYED status.
    [Tags]    voltha-0001
    : FOR    ${i}    IN    @{charts}
    \    ${rc}=    Run and Return Rc    helm list | grep ${i} | grep -i deployed;
    \    Should Be Equal As Integers    ${rc}    0

Validate K8 Nodes
    [Documentation]    Validates that all nodes that are running in the K8 are healthy after deployment
    [Tags]    voltha-0001
    ${deployed_nodes}=    Run    kubectl get nodes -o json
    Log    ${deployed_nodes}
    @{nodes}=    Get Names    ${deployed_nodes}
    Log    ${nodes}
    #validates that all expected nodes to be running
    : FOR    ${i}    IN    @{nodes}
    \    List Should Contain Value    ${nodes}    ${i}
    : FOR    ${i}    IN    @{nodes}
    \    ${status}=     Run    kubectl get nodes ${i} -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
    \    ${pidpressure}=     Run    kubectl get nodes ${i} -o jsonpath='{.status.conditions[?(@.type=="PIDPressure")].status}'
    \    ${memorypressure}=     Run    kubectl get nodes ${i} -o jsonpath='{.status.conditions[?(@.type=="MemoryPressure")].status}'
    \    ${diskpressure}=     Run    kubectl get nodes ${i} -o jsonpath='{.status.conditions[?(@.type=="DiskPressure")].status}'
    \    Should Be Equal    ${status}    True
    \    Should Be Equal    ${pidpressure}    False
    \    Should Be Equal    ${memorypressure}    False
    \    Should Be Equal    ${diskpressure}    False

Validate Voltha Pods
    [Documentation]    Validates that all expected containers that are running in the K8 Pods and healthy
    [Tags]    voltha-0001
    @{container_names}=    Run Keyword and Continue on Failure    Validate Pods
    #validates that all expected containers to be running are in one of the pods inspected above
    : FOR    ${i}    IN    @{pods}
    \    Run Keyword and Continue on Failure    List Should Contain Value    ${container_names}    ${i}

Validate Voltha Deployments
    [Documentation]    Validates that all voltha deployments successfully rolled out and available
    [Tags]    voltha-0001
    Validate Deployments    ${deployments}

Validate Voltha Services
    [Documentation]    Validates that all expected voltha services that are running in K8s
    [Tags]    voltha-0001
    ${voltha_services}=    Run    kubectl get services -o json -n voltha
    Log    ${voltha_services}
    @{voltha_services}=    Get Names    ${voltha_services}
    #validates that all expected services are running
    : FOR    ${i}    IN    @{services}
    \    Run Keyword and Continue on Failure    List Should Contain Value    ${voltha_services}    ${i}

*** Keywords ***
Get Names
    [Documentation]    Gets names of K8 resources running
    [Arguments]    ${output}
    @{names}=    Create List
    ${output}=    To JSON    ${output}
    ${len}=    Get Length    ${output}
    ${length}=    Get Length    ${output['items']}
    : FOR    ${INDEX}    IN RANGE    0    ${length}
    \    ${item}=    Get From List    ${output['items']}    ${INDEX}
    \    ${metadata}=    Get From Dictionary    ${item}    metadata
    \    ${name}=    Get From Dictionary    ${metadata}    name
    \    Append To List    ${names}    ${name}
    [Return]    @{names}

Validate Pods
    @{container_names}=    Create List
    ${pods}=    Run    kubectl get pods -o json -n voltha
    Log    ${pods}
    ${pods}=    To JSON    ${pods}
    ${len}=    Get Length    ${pods}
    ${length}=    Get Length    ${pods['items']}
    : FOR    ${INDEX}    IN RANGE    0    ${length}
    \    ${item}=    Get From List    ${pods['items']}    ${INDEX}
    \    ${metadata}=    Get From Dictionary    ${item}    metadata
    \    ${name}=    Get From Dictionary    ${metadata}    name
    \    ${status}=    Get From Dictionary    ${item}    status
    \    ${containerStatuses}=    Get From Dictionary    ${status}    containerStatuses
    \    Log    ${containerStatuses}
    \    ${cstatus}=    Get From List    ${containerStatuses}    0
    \     Log    ${cstatus}
    \    ${restartCount}=    Get From Dictionary    ${cstatus}    restartCount
    \    Run Keyword and Continue On Failure    Should Be Equal As Integers    ${restartCount}    0
    \    ${container_name}=    Get From Dictionary    ${cstatus}    name
    \    ${state}=    Get From Dictionary    ${cstatus}    state
    \    Run Keyword and Continue On Failure    Should Contain    ${state}    running
    \    Run Keyword and Continue On Failure    Should Not Contain    ${state}    stopped
    \    Log    ${state}
    \    Append To List    ${container_names}    ${container_name}
    [Return]    ${container_names}

Validate Deployments
    [Arguments]    ${expected_deployments}
    ${deplymts}=    Run    kubectl get deployments -o json -n voltha
    @{deplymts}=    Get Names    ${deplymts}
    : FOR    ${i}    IN    @{deplymts}
    \    ${rollout_status}=    Run    kubectl rollout status deployment/${i} -n voltha
    \    Run Keyword and Continue On Failure    Should Be Equal    ${rollout_status}    deployment "${i}" successfully rolled out
    \    ##validate replication sets
    \    ${desired}=    Run    kubectl get deployments ${i} -o jsonpath='{.status.replicas}'
    \    ${available}=    Run    kubectl get deployments ${i} -o jsonpath='{.status.availableReplicas}'
    \    Run Keyword and Continue On Failure    Should Be Equal    ${desired}    ${available}
    #validates that all expected deployments to exist
    : FOR    ${i}    IN    @{expected_deployments}
    \    Run Keyword and Continue On Failure    List Should Contain Value    ${deplymts}    ${i}