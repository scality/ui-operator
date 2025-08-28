# End-to-End Testing Plan for UI Operator

## Overview

This document outlines the comprehensive End-to-End (E2E) testing strategy for the Scality UI Operator. The UI Operator manages three core Custom Resource Definitions (CRDs): ScalityUI, ScalityUIComponent, and ScalityUIComponentExposer, orchestrating the deployment and configuration of a micro-frontend architecture for Scality products.

## System Architecture Analysis

### Core Components
1. **ScalityUI**: Main UI shell providing authentication, theming, and navigation framework
2. **ScalityUIComponent**: Individual micro-frontend components/modules
3. **ScalityUIComponentExposer**: Configuration layer exposing components through the main UI shell
4. **UI Operator**: Kubernetes controller managing the entire lifecycle

### Key Workflows
- **Deployment Lifecycle**: CRD creation → Controller reconciliation → Kubernetes resource deployment
- **Configuration Management**: ConfigMap generation and injection for runtime configuration  
- **Network Exposure**: Ingress configuration with authentication and routing
- **Component Integration**: Dynamic component discovery and registration

## Test Environment Requirements

### Infrastructure Prerequisites
- **Kubernetes Cluster**: v1.11.3+ (Kind cluster for CI/CD)
- **Container Runtime**: Docker 17.03+
- **CLI Tools**: kubectl, make, go 1.24+
- **Dependencies**: Prometheus Operator, Cert-Manager

### Test Data Management
- **Fixtures Directory**: `/test/e2e/fixtures/`
  - `scalityui/`: Main UI shell configurations
  - `components/`: Component definitions and manifests
  - `exposers/`: Component exposure configurations
- **Dynamic Test Data**: Generated configurations for various scenarios

## Comprehensive Test Scenarios

### 1. Operator Lifecycle Tests

#### 1.1 Operator Deployment and Health
**Objective**: Verify operator can be deployed and runs successfully

**Test Steps**:
1. Build operator image (`make docker-build`)
2. Load image to test cluster (`kind load docker-image`)
3. Install CRDs (`make install`)
4. Deploy operator (`make deploy`)
5. Verify operator pod is running and ready
6. Check operator logs for startup errors
7. Validate CRD registration in cluster

**Expected Results**:
- Operator pod status: `Running`
- No error logs during startup
- All three CRDs registered and available
- Controller-manager service accessible

**Test Data**:
```yaml
# Deployment validation
namespace: ui-operator-system
operator-image: example.com/ui-operator:v0.0.1
controller-selector: control-plane=controller-manager
```

#### 1.2 Operator Upgrade and Rollback
**Objective**: Ensure operator can be upgraded safely

**Test Steps**:
1. Deploy operator version N
2. Create sample CRs and verify functionality
3. Upgrade to version N+1
4. Verify existing CRs continue working
5. Test new features if applicable
6. Rollback to version N
7. Verify rollback success

### 2. ScalityUI Core Tests

#### 2.1 Basic ScalityUI Deployment
**Objective**: Verify complete ScalityUI deployment workflow

**Test Steps**:
1. Apply ScalityUI CR with minimal configuration
2. Monitor reconciliation process
3. Verify Deployment creation with correct spec
4. Verify Service creation and endpoints
5. Verify ConfigMap creation with UI configuration
6. Check pod startup and readiness
7. Validate status updates in CR

**Test Data**:
```yaml
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUI
metadata:
  name: test-ui
spec:
  image: "scality/ui:latest"
  productName: "Test Product"
  themes:
    light:
      type: "core-ui"
      name: "light-theme"
      logo:
        type: "path"
        value: "/assets/logo.png"
    dark:
      type: "core-ui"  
      name: "dark-theme"
      logo:
        type: "svg"
        value: "<svg>...</svg>"
```

#### 2.2 Advanced ScalityUI Configuration
**Objective**: Test complex configuration scenarios

**Test Areas**:
- **Authentication Configuration**: OIDC, Basic Auth, No Auth modes
- **Network Configuration**: Custom ingress, TLS, annotations
- **Theming**: Light/dark themes, custom logos, branding
- **Navbar Configuration**: Internal/external links, user groups, localization
- **Pod Scheduling**: Node selectors, affinity rules, tolerations

**OIDC Authentication Test**:
```yaml
spec:
  auth:
    kind: "OIDC"
    providerUrl: "https://test-oidc.example.com"
    clientId: "test-client-id"
    redirectUrl: "/dashboard"
    scopes: "openid email profile groups"
    providerLogout: true
```

**Network Configuration Test**:
```yaml
spec:
  networks:
    ingressClassName: "nginx"
    host: "ui.test.example.com"
    tls:
      - secretName: "ui-tls-secret"
        hosts: ["ui.test.example.com"]
    ingressAnnotations:
      nginx.ingress.kubernetes.io/rewrite-target: "/"
      cert-manager.io/cluster-issuer: "letsencrypt-prod"
```

#### 2.3 ScalityUI Status and Error Handling
**Objective**: Verify proper status reporting and error conditions

**Test Scenarios**:
1. **Image Pull Failures**: Invalid image, missing pull secrets
2. **Configuration Errors**: Invalid OIDC settings, malformed navbar config
3. **Resource Constraints**: Insufficient cluster resources
4. **Network Issues**: Invalid ingress configuration

### 3. ScalityUIComponent Tests

#### 3.1 Component Deployment
**Objective**: Verify individual component deployment

**Test Steps**:
1. Deploy ScalityUIComponent with minimal config
2. Verify Deployment creation
3. Check container image and mount configuration
4. Validate component startup and health
5. Verify status reporting

**Test Data**:
```yaml
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUIComponent
metadata:
  name: test-component
  namespace: ui-components
spec:
  image: "scality/test-component:v1.0.0"
  mountPath: "/app/config"
  scheduling:
    nodeSelector:
      component-type: "ui"
```

#### 3.2 Component Lifecycle Management
**Objective**: Test component updates and scaling

**Test Scenarios**:
1. **Image Updates**: Change component image version
2. **Configuration Changes**: Modify mount paths, scheduling
3. **Resource Scaling**: Update replicas, resources
4. **Pod Scheduling**: Test affinity, tolerations, node selection

### 4. ScalityUIComponentExposer Tests

#### 4.1 Component Exposure and Integration
**Objective**: Verify component exposure through main UI

**Test Steps**:
1. Deploy ScalityUI instance
2. Deploy ScalityUIComponent  
3. Deploy ScalityUIComponentExposer linking them
4. Verify ConfigMap creation in UI namespace
5. Verify UI deployment update with new config
6. Test component accessibility through UI
7. Validate configuration injection

**Test Data**:
```yaml
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUIComponentExposer
metadata:
  name: test-exposer
  namespace: ui-components
spec:
  scalityUI: "main-ui"
  scalityUIComponent: "test-component"
  appHistoryBasePath: "/test-app"
  auth:
    kind: "OIDC"
    providerUrl: "https://auth.test.com"
    clientId: "component-client"
  selfConfiguration:
    apiEndpoint: "https://api.test.com"
    features: ["feature1", "feature2"]
```

#### 4.2 Multi-Component Integration
**Objective**: Test multiple components exposed through single UI

**Test Scenarios**:
1. Deploy multiple UIComponents
2. Create multiple Exposers for same ScalityUI
3. Verify all components registered in UI config
4. Test component isolation and communication
5. Validate routing and path management

### 5. End-to-End Integration Tests

#### 5.1 Complete Application Stack
**Objective**: Test full application deployment and integration

**Test Flow**:
1. **Environment Setup**:
   - Deploy operator
   - Install dependencies (cert-manager, prometheus)
   - Configure ingress controller

2. **Application Deployment**:
   - Deploy ScalityUI with full configuration
   - Deploy multiple ScalityUIComponents
   - Configure ScalityUIComponentExposers
   - Set up ingress and TLS

3. **Functional Validation**:
   - Access UI through ingress
   - Verify authentication flow
   - Test component loading and routing
   - Validate theming and branding
   - Test user navigation and permissions

#### 5.2 Configuration Management Integration
**Objective**: Test dynamic configuration updates

**Test Scenarios**:
1. **Runtime Updates**: Modify ScalityUI configuration, verify live updates
2. **Component Addition**: Add new component, verify automatic registration  
3. **Component Removal**: Remove component, verify cleanup
4. **Authentication Changes**: Modify auth config, test immediate effect

### 6. Performance and Scalability Tests

#### 6.1 Resource Usage Validation
**Objective**: Ensure operator performs within resource constraints

**Test Metrics**:
- Operator CPU/Memory usage under load
- Reconciliation loop performance
- Kubernetes API call efficiency
- Controller cache effectiveness

#### 6.2 Scale Testing
**Objective**: Test operator behavior with multiple resources

**Test Scenarios**:
1. Deploy 10+ ScalityUI instances
2. Deploy 50+ ScalityUIComponents
3. Create 100+ ScalityUIComponentExposers
4. Measure reconciliation times and resource usage

### 7. Failure and Recovery Tests

#### 7.1 Operator Resilience
**Objective**: Test operator behavior during failures

**Test Scenarios**:
1. **Operator Restart**: Kill operator pod, verify state recovery
2. **Kubernetes API Failures**: Simulate API downtime
3. **Network Partitions**: Test partial cluster connectivity
4. **Resource Deletion**: Delete managed resources, verify recreation

#### 7.2 Data Consistency
**Objective**: Ensure data integrity during failures

**Test Areas**:
- ConfigMap consistency across restarts
- Status field accuracy after recovery
- Finalizer handling during deletion
- Cross-namespace resource management

### 8. Security Testing

#### 8.1 RBAC Validation
**Objective**: Verify proper permission boundaries

**Test Steps**:
1. Validate operator ServiceAccount permissions
2. Test resource access restrictions
3. Verify cross-namespace access controls
4. Test user authentication integration

#### 8.2 Configuration Security
**Objective**: Test secure configuration handling

**Test Areas**:
- Secret reference handling
- Image pull secret management
- TLS certificate management
- Authentication token handling

### 9. Edge Cases and Special Scenarios

#### 9.1 Resource Conflict Resolution
**Objective**: Test handling of resource naming conflicts and ownership

**Edge Cases**:
1. **Duplicate Resource Names**:
   - Deploy two ScalityUI instances with same name in different phases
   - Create ScalityUIComponent with same name as existing Deployment
   - Test finalizer conflicts during concurrent deletion

2. **Cross-Namespace Resource Conflicts**:
   - Components referencing non-existent ScalityUI across namespaces
   - Multiple exposers targeting same component from different namespaces
   - Ingress path conflicts between multiple UI instances

3. **Resource Owner Reference Edge Cases**:
   - Manual deletion of owned resources while CR exists
   - Orphaned resources when parent CR is deleted without finalizers
   - Owner reference cycles between related resources

**Test Data**:
```yaml
# Scenario: Conflicting resource names
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUI
metadata:
  name: conflict-test
  namespace: ui-system-1
spec:
  image: "scality/ui:v1.0"
  productName: "Conflict Test UI"
---
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUIComponent
metadata:
  name: conflict-test  # Same name as UI
  namespace: ui-system-2
spec:
  image: "scality/component:v1.0"
  mountPath: "/app/config"
```

#### 9.2 Malformed Configuration Handling
**Objective**: Ensure graceful handling of invalid configurations

**Edge Cases**:
1. **Invalid JSON/YAML in SelfConfiguration**:
   - Malformed JSON in ScalityUIComponentExposer.spec.selfConfiguration
   - Non-serializable data structures
   - Extremely large configuration objects (>1MB)

2. **Invalid Image References**:
   - Non-existent registry URLs
   - Images with invalid tags or SHA digests
   - Registry with authentication failures
   - Images that exist but fail health checks

3. **Circular Dependencies**:
   - Component exposer referencing itself
   - UI component depending on its own output
   - Chain of dependencies creating cycles

**Test Scenarios**:
```yaml
# Invalid self-configuration test
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUIComponentExposer
metadata:
  name: malformed-config-test
spec:
  scalityUI: "main-ui"
  scalityUIComponent: "test-component"
  appHistoryBasePath: "/invalid"
  selfConfiguration:
    invalidJson: '{"unclosed": "object'
    circularRef: *self
    largeConfig: # 2MB+ of configuration data
```

#### 9.3 Resource Limit Boundary Testing
**Objective**: Test behavior at resource limits and constraints

**Edge Cases**:
1. **Memory and CPU Limits**:
   - Pods exceeding configured resource limits
   - Out-of-Memory (OOM) kills during startup
   - CPU throttling affecting reconciliation performance
   - Resource quota exhaustion in namespace

2. **Storage and Volume Limits**:
   - ConfigMap size limits (1MB+ configurations)
   - PersistentVolume storage exhaustion
   - EmptyDir volume size limits
   - Mount path conflicts

3. **Network Limits**:
   - Port exhaustion scenarios
   - Service IP allocation failures
   - Ingress controller limits
   - DNS resolution failures

**Test Scenarios**:
```yaml
# Resource limit boundary test
apiVersion: ui.scality.com/v1alpha1
kind: ScalityUI
metadata:
  name: resource-limit-test
spec:
  image: "scality/ui:memory-hog"
  productName: "Resource Test"
  # Force resource constraints
  scheduling:
    resources:
      limits:
        memory: "64Mi"  # Deliberately low
        cpu: "50m"
      requests:
        memory: "32Mi"
        cpu: "25m"
```

#### 9.4 Timing and Race Condition Tests
**Objective**: Test concurrent operations and timing-sensitive scenarios

**Edge Cases**:
1. **Rapid CR Creation/Deletion**:
   - Create and delete CRs in rapid succession
   - Multiple operators reconciling same resources
   - Controller restart during reconciliation
   - Webhook failures during validation

2. **Dependency Timing Issues**:
   - Component exposer created before referenced component
   - UI component referencing non-existent UI instance
   - External dependencies (ingress controller) not ready

3. **Network Timing Problems**:
   - Service endpoints not ready during ingress creation
   - DNS propagation delays
   - Certificate provisioning delays
   - LoadBalancer IP assignment timing

**Test Implementation**:
```bash
#!/bin/bash
# Race condition test script
for i in {1..10}; do
  kubectl apply -f test-ui-${i}.yaml &
  kubectl apply -f test-component-${i}.yaml &
  kubectl apply -f test-exposer-${i}.yaml &
done
wait
# Verify all resources created correctly
```

#### 9.5 Version Compatibility Edge Cases
**Objective**: Test compatibility across different versions and migrations

**Edge Cases**:
1. **API Version Migrations**:
   - Upgrade from v1alpha1 to v1beta1 (future)
   - Deprecated field handling
   - Schema validation changes
   - Custom resource validation webhook changes

2. **Kubernetes Version Compatibility**:
   - Minimum supported Kubernetes version (v1.11.3)
   - Maximum tested Kubernetes version
   - API deprecations in newer Kubernetes versions
   - Feature gate dependencies

3. **Container Runtime Compatibility**:
   - Docker vs containerd vs CRI-O
   - Different container image formats
   - Security context compatibility
   - Volume mount behavior differences

#### 9.6 Multi-Tenancy and Isolation Edge Cases
**Objective**: Test tenant isolation and cross-tenant interference

**Edge Cases**:
1. **Namespace Isolation Violations**:
   - Components accessing secrets from other namespaces
   - Service discovery across namespace boundaries
   - Network policy violations
   - RBAC bypass attempts

2. **Resource Name Collisions**:
   - Multiple tenants using same resource names
   - Global resources (ClusterRole, CRD) conflicts
   - Ingress hostname conflicts
   - LoadBalancer service conflicts

3. **Configuration Leakage**:
   - Sensitive data appearing in logs
   - Configuration data visible in wrong namespaces
   - Authentication tokens shared between tenants

#### 9.7 Disaster Recovery Edge Cases
**Objective**: Test system recovery from catastrophic failures

**Edge Cases**:
1. **Complete Cluster Failure**:
   - Cluster recreated from backup
   - Persistent volume data recovery
   - External service reconnection
   - State synchronization after downtime

2. **Operator Corruption Scenarios**:
   - Operator image corruption
   - CRD corruption or deletion
   - Controller cache corruption
   - Webhook certificate expiration

3. **External Dependency Failures**:
   - Container registry unavailable
   - DNS service failures
   - Load balancer provider failures
   - Certificate authority unavailable

#### 9.8 Performance Degradation Edge Cases
**Objective**: Test system behavior under performance stress

**Edge Cases**:
1. **High Reconciliation Load**:
   - 1000+ CRs triggering simultaneous reconciliation
   - Controller manager CPU/memory exhaustion
   - Kubernetes API server rate limiting
   - Event flooding scenarios

2. **Large Configuration Objects**:
   - ConfigMaps exceeding 1MB
   - Extremely complex navbar configurations
   - Large theme definitions with embedded images
   - Massive selfConfiguration objects

3. **Network Latency Issues**:
   - High latency to container registry
   - Slow DNS resolution
   - Intermittent network partitions
   - Bandwidth constraints

#### 9.9 Security Boundary Edge Cases
**Objective**: Test security controls under extreme conditions

**Edge Cases**:
1. **Privilege Escalation Attempts**:
   - Container breakout attempts
   - ServiceAccount token abuse
   - Host path mount exploitation
   - Network policy bypass attempts

2. **Input Validation Bypass**:
   - Webhook validation circumvention
   - Schema validation edge cases
   - Regular expression injection
   - Path traversal in mount paths

3. **Certificate and TLS Edge Cases**:
   - Expired certificates during operation
   - Certificate chain validation failures
   - TLS version compatibility issues
   - Cipher suite negotiation failures

## Test Implementation Framework

### Test Structure
```
test/e2e/
├── e2e_suite_test.go          # Test suite setup
├── e2e_test.go                # Main test file
├── scenarios/                 # Test scenarios
│   ├── operator_lifecycle_test.go
│   ├── scalityui_test.go
│   ├── component_test.go
│   ├── exposer_test.go
│   ├── integration_test.go
│   ├── performance_test.go
│   └── security_test.go
├── fixtures/                  # Test data
│   ├── scalityui/
│   ├── components/
│   └── exposers/
└── helpers/                   # Test utilities
    ├── cluster.go
    ├── resources.go
    └── validation.go
```

### Test Utilities and Helpers

#### Cluster Management
```go
// Helper functions for cluster operations
func SetupTestCluster() error
func TeardownTestCluster() error
func WaitForOperatorReady() error
func ApplyFixture(fixturePath string) error
```

#### Resource Validation
```go
// Helper functions for resource validation
func ValidateDeployment(name, namespace string) error
func ValidateService(name, namespace string) error
func ValidateIngress(name, namespace string) error
func ValidateConfigMap(name, namespace string) error
```

#### Status Checking
```go
// Helper functions for status validation
func WaitForCRReady(cr client.Object) error
func ValidateConditions(conditions []metav1.Condition) error
func CheckResourceHealth(resource client.Object) error
```

### Test Execution Strategy

#### Parallel vs Sequential Execution
- **Parallel**: Independent operator lifecycle tests
- **Sequential**: Integration tests requiring shared resources
- **Isolated**: Tests requiring specific cluster states

#### Test Data Management
- **Static Fixtures**: Pre-defined YAML manifests for common scenarios
- **Dynamic Generation**: Programmatic test data for parameterized tests
- **Cleanup Strategy**: Proper resource cleanup after each test

#### Continuous Integration Integration
```yaml
# Example GitHub Actions workflow
name: E2E Tests
on: [push, pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: 1.24
      - name: Create Kind Cluster
        run: |
          kind create cluster
          kubectl cluster-info
      - name: Run E2E Tests
        run: |
          make test-e2e
```

## Test Coverage Requirements

### Functional Coverage
- ✅ All CRD fields and configurations
- ✅ All controller reconciliation paths
- ✅ All status conditions and error states
- ✅ All integration points between components

### Error Scenario Coverage
- ✅ Invalid configurations
- ✅ Resource failures
- ✅ Network issues
- ✅ Permission errors

### Performance Coverage
- ✅ Resource usage under normal load
- ✅ Reconciliation performance
- ✅ Scale limits and boundaries
- ✅ Recovery time objectives

## Success Criteria

### Test Pass Criteria
1. **100% Test Execution**: All defined test scenarios execute successfully
2. **Resource Cleanup**: No resource leaks after test completion  
3. **Performance Targets**: Reconciliation time < 30s for standard deployments
4. **Error Recovery**: System recovers within 60s from common failure scenarios
5. **Security Validation**: All RBAC and security controls function correctly

### Quality Gates
1. **Code Coverage**: Minimum 80% coverage for controller logic
2. **Test Reliability**: Maximum 5% flaky test rate
3. **Execution Time**: Full E2E suite completes within 30 minutes
4. **Resource Usage**: Test cluster resources < 4GB RAM, 2 CPU cores

## Maintenance and Evolution

### Test Maintenance Strategy
- **Regular Updates**: Update tests with new features and configurations
- **Deprecation Handling**: Graceful handling of deprecated APIs and fields  
- **Version Compatibility**: Test across supported Kubernetes versions
- **Documentation Updates**: Keep test documentation synchronized with code changes

### Continuous Improvement
- **Failure Analysis**: Analyze test failures for system improvements
- **Performance Monitoring**: Track test execution performance over time
- **Coverage Analysis**: Regular review of test coverage and gaps
- **Community Feedback**: Incorporate feedback from operator users and contributors

This comprehensive E2E testing plan ensures the UI Operator functions reliably across all supported scenarios and provides confidence in production deployments.