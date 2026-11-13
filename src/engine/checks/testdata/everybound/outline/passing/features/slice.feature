Feature: greeting

  
  Scenario Outline: greet many
    Given I greet "<name>"
    Then it works

    
    Examples:
      | name |
      | ann  |
      | bob  |

    
    Examples:
      | name |
      | cy   |

  
  Scenario: greet nobody
    Given I do the thing
    Then it works

