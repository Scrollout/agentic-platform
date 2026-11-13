Feature: Greetings

  Rule: R-1 A greeting names the person
    @R-1 @E-1
    Scenario: Greet Ada
      When I ask for the greeting for "Ada"
      Then the text is "Hello, Ada"
