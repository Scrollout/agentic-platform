Feature: greeting

  Rule: R-1 An unbound step
    @R-1 @E-1
    Scenario: unbound
      Given nobody has defined this step
      Then it works

  Rule: R-2 A failing step
    @R-2 @E-2
    Scenario: fails against the stub
      Given I do the thing
      Then it works
